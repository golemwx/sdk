package transport

import (
	"errors"
	"io"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

const (
	// MaxMediaChunkSize 单个媒体传输分块上限
	MaxMediaChunkSize = 256 << 10
	// MaxDefaultMediaSize core 未声明更小上限时的媒体总量上限
	MaxDefaultMediaSize = 64 << 20
	// MaxEmojiMediaSize 表情处理的明确总量上限
	MaxEmojiMediaSize = 10 << 20
	// maxMediaFileNameSize 展示文件名的 UTF-8 字节上限
	maxMediaFileNameSize = 255
)

// mediaOperation 统一媒体写操作的错误定位
const mediaOperation = "messages.send_media"

// MediaSizeLimit 返回指定媒体类型的总量上限
func MediaSizeLimit(kind sdk.MediaKind) (int64, bool) {
	switch kind {
	case sdk.MediaKindEmoji:
		return MaxEmojiMediaSize, true
	case sdk.MediaKindImage, sdk.MediaKindVoice, sdk.MediaKindVideo, sdk.MediaKindFile:
		return MaxDefaultMediaSize, true
	default:
		return 0, false
	}
}

// ValidateMediaUpload 校验 Reader 上传的元数据和公共类型边界
func ValidateMediaUpload(upload message.MediaUpload) error {
	limit, supported := MediaSizeLimit(upload.Kind)
	if !supported {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "unsupported media kind")
	}
	if upload.Ref != nil {
		if upload.Reader != nil {
			return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "media reference and reader are mutually exclusive")
		}
		if upload.Ref.Token == "" || upload.Ref.Kind != upload.Kind || upload.Ref.SizeBytes < -1 ||
			upload.Ref.SizeBytes == 0 || upload.Ref.SizeBytes > limit || upload.Ref.ExpiresAt.IsZero() {
			return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "invalid media reference")
		}
		if upload.Voice != nil {
			return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "voice options are stored in the media reference")
		}
		return nil
	}
	if upload.Reader == nil {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "missing media reader")
	}
	if upload.Size < -1 || upload.Size == 0 {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "invalid media size")
	}
	if upload.Size > limit {
		return mediaError(sdk.CategoryResourceExhausted, sdk.OutcomeFailed, "media size exceeds limit")
	}
	if upload.Kind == sdk.MediaKindVoice {
		if upload.Voice == nil || upload.Voice.Duration <= 0 || upload.Voice.Duration%time.Millisecond != 0 ||
			upload.Voice.Duration/time.Millisecond > 1<<31-1 ||
			upload.Voice.Encoding < message.VoiceEncodingAMR || upload.Voice.Encoding > message.VoiceEncodingSILK {
			return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "invalid voice options")
		}
	} else if upload.Voice != nil {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "voice options require voice media")
	}
	encoding := message.VoiceEncodingUnknown
	if upload.Voice != nil {
		encoding = upload.Voice.Encoding
	}
	if !validMediaFileName(upload.FileName, upload.Kind, encoding) {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "invalid media file name")
	}
	return nil
}

// ValidateMediaStore 校验只生成引用、不发送消息的 Reader 媒体
func ValidateMediaStore(upload message.MediaUpload) error {
	if upload.Ref != nil {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "media store requires a reader")
	}
	return ValidateMediaUpload(upload)
}

// ValidateMediaTotal 校验累计字节数及可选声明长度
func ValidateMediaTotal(kind sdk.MediaKind, declared, actual int64, complete bool) error {
	limit, supported := MediaSizeLimit(kind)
	if !supported {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "unsupported media kind")
	}
	if actual > limit {
		return mediaError(sdk.CategoryResourceExhausted, sdk.OutcomeUnknown, "media size exceeds limit")
	}
	if declared >= 0 && actual > declared || complete && declared >= 0 && actual != declared {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeUnknown, "declared media size does not match stream")
	}
	if complete && actual == 0 {
		return mediaError(sdk.CategoryInvalidArgument, sdk.OutcomeFailed, "media stream is empty")
	}
	return nil
}

// ValidateMediaDownloadTotal 校验宿主下载流，不把读取错误标记为发送副作用
func ValidateMediaDownloadTotal(kind sdk.MediaKind, declared, actual int64, complete bool) error {
	limit, supported := MediaSizeLimit(kind)
	if !supported {
		return sdk.NewError(sdk.CategoryInvalidArgument, "messages.download", "unsupported media kind")
	}
	if actual > limit {
		return sdk.NewError(sdk.CategoryResourceExhausted, "messages.download", "media size exceeds limit")
	}
	if declared >= 0 && actual > declared || complete && declared >= 0 && actual != declared {
		return sdk.NewError(sdk.CategoryInternal, "messages.download", "media size does not match reference")
	}
	if complete && actual == 0 {
		return sdk.NewError(sdk.CategoryInternal, "messages.download", "media stream is empty")
	}
	return nil
}

// ValidateMediaStoreTotal 校验只产生临时引用的媒体流
func ValidateMediaStoreTotal(kind sdk.MediaKind, declared, actual int64, complete bool) error {
	limit, supported := MediaSizeLimit(kind)
	if !supported {
		return sdk.NewError(sdk.CategoryInvalidArgument, "messages.store_media", "unsupported media kind")
	}
	if actual > limit {
		return sdk.NewError(sdk.CategoryResourceExhausted, "messages.store_media", "media size exceeds limit")
	}
	if declared >= 0 && actual > declared || complete && declared >= 0 && actual != declared {
		return sdk.NewError(sdk.CategoryInvalidArgument, "messages.store_media", "declared media size does not match stream")
	}
	if complete && actual == 0 {
		return sdk.NewError(sdk.CategoryInvalidArgument, "messages.store_media", "media stream is empty")
	}
	return nil
}

// ReadMediaChunk 读取一个有界分块，并拒绝长期无进展的异常 Reader
func ReadMediaChunk(reader io.Reader, buffer []byte, emptyReads *int) (int, error) {
	n, err := reader.Read(buffer)
	if n < 0 || n > len(buffer) {
		return 0, errors.New("media reader returned an invalid byte count")
	}
	if n > 0 {
		*emptyReads = 0
		return n, err
	}
	if err != nil {
		return 0, err
	}
	*emptyReads = *emptyReads + 1
	if *emptyReads >= 100 {
		return 0, io.ErrNoProgress
	}
	return 0, nil
}

// validMediaFileName 限制文件名为单个展示名称，并按媒体类型限制可识别扩展名
func validMediaFileName(name string, kind sdk.MediaKind, encoding message.VoiceEncoding) bool {
	if name == "" || len(name) > maxMediaFileNameSize || !utf8.ValidString(name) ||
		strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\:<>"|?*`) ||
		strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) }) {
		return false
	}
	rawExtension := path.Ext(name)
	base := strings.TrimSuffix(name, rawExtension)
	if base == "" || base == "." || base == ".." || strings.HasSuffix(base, ".") || strings.HasSuffix(base, " ") {
		return false
	}
	deviceName := strings.ToUpper(strings.SplitN(base, ".", 2)[0])
	switch deviceName {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	extension := strings.ToLower(rawExtension)
	if extension == "" {
		return true
	}
	if len(extension) > 17 || extension[0] != '.' ||
		strings.ContainsFunc(extension[1:], func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		}) {
		return false
	}
	switch kind {
	case sdk.MediaKindImage:
		return oneOf(extension, ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp")
	case sdk.MediaKindEmoji:
		return oneOf(extension, ".jpg", ".jpeg", ".png", ".gif")
	case sdk.MediaKindVideo:
		return oneOf(extension, ".mp4", ".m4v", ".mov", ".avi", ".mkv", ".webm")
	case sdk.MediaKindVoice:
		switch encoding {
		case message.VoiceEncodingAMR:
			return extension == ".amr"
		case message.VoiceEncodingSPEEX:
			return oneOf(extension, ".spx", ".speex")
		case message.VoiceEncodingMP3:
			return extension == ".mp3"
		case message.VoiceEncodingWAVE:
			return oneOf(extension, ".wav", ".wave")
		case message.VoiceEncodingSILK:
			return oneOf(extension, ".silk", ".slk")
		default:
			return false
		}
	case sdk.MediaKindFile:
		return true
	default:
		return false
	}
}

// oneOf 判断值是否属于允许的固定集合
func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

// mediaError 为写操作保留可机器判断的副作用结果
func mediaError(category sdk.Category, outcome sdk.WriteOutcome, reason string) *sdk.Error {
	value := sdk.NewError(category, mediaOperation, reason)
	value.Outcome = outcome
	return value
}
