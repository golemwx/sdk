package coreapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/golemwx/sdk"
)

// MessageClient 声明当前 core 消息域的全部业务入口。
// 实时同步仍通过 SDK 事件订阅提供，不在此重复建立第二条事件流。
type MessageClient Client

func validateMediaRef(method Method, ref sdk.MediaRef, kinds ...sdk.MediaKind) error {
	if ref.Token == "" || ref.SizeBytes < -1 || ref.ExpiresAt.IsZero() {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "media reference is invalid")
	}
	for _, kind := range kinds {
		if ref.Kind == kind {
			return nil
		}
	}
	return sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "media reference kind is invalid")
}

func validateForwardRef(method Method, ref sdk.ForwardRef) error {
	if ref.Token == "" || ref.ExpiresAt.IsZero() {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "forward reference is invalid")
	}
	return nil
}

func validateResourceRef(method Method, ref ResourceRef, kind string) error {
	if ref.Token == "" || ref.Kind != kind || ref.ExpiresAt.IsZero() {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "resource reference is invalid")
	}
	return nil
}

// SendText 发送文本及群内提及。
func (c *MessageClient) SendText(ctx context.Context, request MessageTextRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.Content == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendText), "receiver and content are required")
	}
	return (*Client)(c).call(ctx, MessageSendText, request)
}

// SendEmoji 发送表情文件或已有 MD5。
func (c *MessageClient) SendEmoji(ctx context.Context, request MessageEmojiRequest) (json.RawMessage, error) {
	if request.Receiver == "" || (request.Ref == nil && request.MD5 == "" && request.URL == "") {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendEmoji), "receiver and one emoji source are required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(MessageSendEmoji, *request.Ref, sdk.MediaKindEmoji, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.URL != "" {
		if err := validHTTPURL(operation(MessageSendEmoji), request.URL); err != nil {
			return nil, err
		}
	}
	return (*Client)(c).call(ctx, MessageSendEmoji, request)
}

// SendImage 发送图片。
func (c *MessageClient) SendImage(ctx context.Context, receiver string, ref sdk.MediaRef) (json.RawMessage, error) {
	if receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendImage), "receiver is required")
	}
	if err := validateMediaRef(MessageSendImage, ref, sdk.MediaKindImage); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MessageSendImage, MessageImageRequest{Receiver: receiver, Ref: &ref})
}

// SendImageURL 从 core 可访问的 HTTP(S) 地址发送图片。
func (c *MessageClient) SendImageURL(ctx context.Context, receiver, rawURL string) (json.RawMessage, error) {
	if receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendImage), "receiver is required")
	}
	if err := validHTTPURL(operation(MessageSendImage), rawURL); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MessageSendImage, MessageImageRequest{Receiver: receiver, URL: rawURL})
}

// SendVoice 发送语音。
func (c *MessageClient) SendVoice(ctx context.Context, request MessageVoiceRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.DurationMS <= 0 || request.Format < 0 || request.Format > 4 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendVoice), "receiver, duration and format are invalid")
	}
	if request.Ref == nil && request.URL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendVoice), "voice source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(MessageSendVoice, *request.Ref, sdk.MediaKindVoice); err != nil {
			return nil, err
		}
	}
	if request.URL != "" {
		if err := validHTTPURL(operation(MessageSendVoice), request.URL); err != nil {
			return nil, err
		}
	}
	return (*Client)(c).call(ctx, MessageSendVoice, request)
}

// SendVideo 发送视频，可选封面引用。
func (c *MessageClient) SendVideo(ctx context.Context, request MessageVideoRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendVideo), "receiver is required")
	}
	if request.Ref == nil && request.VideoURL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendVideo), "video source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(MessageSendVideo, *request.Ref, sdk.MediaKindVideo); err != nil {
			return nil, err
		}
	}
	if request.ThumbRef != nil {
		if err := validateMediaRef(MessageSendVideo, *request.ThumbRef, sdk.MediaKindImage); err != nil {
			return nil, err
		}
	}
	if request.VideoURL != "" {
		if err := validHTTPURL(operation(MessageSendVideo), request.VideoURL); err != nil {
			return nil, err
		}
	}
	if request.ThumbURL != "" {
		if err := validHTTPURL(operation(MessageSendVideo), request.ThumbURL); err != nil {
			return nil, err
		}
	}
	return (*Client)(c).call(ctx, MessageSendVideo, request)
}

// SendApp 发送调用方提供的有界应用 XML。
func (c *MessageClient) SendApp(ctx context.Context, request MessageAppRequest) (json.RawMessage, error) {
	xml := strings.TrimSpace(request.XML)
	if request.Receiver == "" || request.Type == 0 || len(xml) > 2<<20 ||
		(!strings.HasPrefix(xml, "<msg") && !strings.HasPrefix(xml, "<appmsg")) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendApp), "receiver, type and bounded app XML are required")
	}
	request.XML = xml
	return (*Client)(c).call(ctx, MessageSendApp, request)
}

// SendRecord 发送合并聊天记录。
func (c *MessageClient) SendRecord(ctx context.Context, request MessageRecordRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendRecord), "receiver is required")
	}
	if err := inlineJSON(operation(MessageSendRecord), request.Record, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MessageSendRecord, request)
}

// SendNote 发送图文笔记。
func (c *MessageClient) SendNote(ctx context.Context, request MessageNoteRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendNote), "receiver is required")
	}
	if err := inlineJSON(operation(MessageSendNote), request.Note, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MessageSendNote, request)
}

// SendFile 发送文件。
func (c *MessageClient) SendFile(ctx context.Context, request MessageFileRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendFile), "receiver is required")
	}
	if request.Ref == nil && request.URL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendFile), "file source is required")
	}
	if request.Ref != nil {
		if err := validateMediaRef(MessageSendFile, *request.Ref, sdk.MediaKindFile, sdk.MediaKindOther); err != nil {
			return nil, err
		}
	}
	if request.URL != "" {
		if err := validHTTPURL(operation(MessageSendFile), request.URL); err != nil {
			return nil, err
		}
	}
	return (*Client)(c).call(ctx, MessageSendFile, request)
}

// SendLink 发送链接卡片。
func (c *MessageClient) SendLink(ctx context.Context, request MessageLinkRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.Title == "" || request.Desc == "" || request.URL == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendLink), "receiver, title, description and URL are required")
	}
	return (*Client)(c).call(ctx, MessageSendLink, request)
}

// SendMusic 发送音乐卡片。
func (c *MessageClient) SendMusic(ctx context.Context, request MessageMusicRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendMusic), "receiver is required")
	}
	if err := inlineJSON(operation(MessageSendMusic), request.Music, true); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, MessageSendMusic, request)
}

// SendPosition 发送位置卡片。
func (c *MessageClient) SendPosition(ctx context.Context, request MessagePositionRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.Name == "" || request.Label == "" ||
		request.Lon == 0 || request.Lat == 0 || request.Scale == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendPosition), "receiver, coordinates, scale, name and label are required")
	}
	return (*Client)(c).call(ctx, MessageSendPosition, request)
}

// SendNameCard 发送联系人名片。
func (c *MessageClient) SendNameCard(ctx context.Context, request MessageNameCardRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.Username == "" || request.Nickname == "" || request.Alias == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageSendNameCard), "receiver, username, nickname and alias are required")
	}
	return (*Client)(c).call(ctx, MessageSendNameCard, request)
}

// Revoke 使用 core 直接撤回字段撤回消息；优先使用 Messages.Revoke 的不透明引用。
func (c *MessageClient) Revoke(ctx context.Context, request MessageRevokeRequest) (json.RawMessage, error) {
	if request.Receiver == "" || request.NewID == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageRevoke), "receiver and new_id are required")
	}
	return (*Client)(c).call(ctx, MessageRevoke, request)
}

// DownloadVoice 下载语音并返回宿主媒体引用。
func (c *MessageClient) DownloadVoice(ctx context.Context, request MessageVoiceDownloadRequest) (*sdk.MediaRef, error) {
	if (request.ID == 0 && request.NewID == 0) || request.Size == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageDownloadVoice), "message ID and size are required")
	}
	data, err := (*Client)(c).call(ctx, MessageDownloadVoice, request)
	if err != nil {
		return nil, err
	}
	result, err := decodeResult[sdk.MediaRef](MessageDownloadVoice, data)
	if err != nil {
		return nil, err
	}
	if err := validateMediaRef(MessageDownloadVoice, result, sdk.MediaKindVoice); err != nil {
		return nil, err
	}
	return &result, nil
}

// TranscribeVoice 请求 core 将语音转为文字。
func (c *MessageClient) TranscribeVoice(ctx context.Context, request MessageTranscribeRequest) (string, error) {
	if (request.ID == 0 && request.NewID == 0) || request.Size == 0 || (request.Format != 0 && request.Format != 1 && request.Format != 4) {
		return "", sdk.NewError(sdk.CategoryInvalidArgument, operation(MessageTranscribeVoice), "message ID, size or format is invalid")
	}
	data, err := (*Client)(c).call(ctx, MessageTranscribeVoice, request)
	if err != nil {
		return "", err
	}
	result, err := decodeResult[string](MessageTranscribeVoice, data)
	if err != nil {
		return "", err
	}
	return result, nil
}

// ForwardImage 使用宿主入站消息引用转发图片。
func (c *MessageClient) ForwardImage(ctx context.Context, request MessageForwardRequest) (json.RawMessage, error) {
	return c.forward(ctx, MessageForwardImage, request)
}

// ForwardVideo 使用宿主入站消息引用转发视频。
func (c *MessageClient) ForwardVideo(ctx context.Context, request MessageForwardRequest) (json.RawMessage, error) {
	return c.forward(ctx, MessageForwardVideo, request)
}

// ForwardFile 使用宿主入站消息引用转发文件。
func (c *MessageClient) ForwardFile(ctx context.Context, request MessageForwardRequest) (json.RawMessage, error) {
	return c.forward(ctx, MessageForwardFile, request)
}

func (c *MessageClient) forward(ctx context.Context, method Method, request MessageForwardRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "receiver is required")
	}
	if err := validateForwardRef(method, request.Ref); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, method, request)
}

// ForwardCDNImage 使用宿主 CDN 资源引用投递图片。
func (c *MessageClient) ForwardCDNImage(ctx context.Context, request MessageCDNForwardRequest) (json.RawMessage, error) {
	return c.forwardCDN(ctx, MessageForwardCDNImage, "image", request)
}

// ForwardCDNVideo 使用宿主 CDN 资源引用投递视频。
func (c *MessageClient) ForwardCDNVideo(ctx context.Context, request MessageCDNForwardRequest) (json.RawMessage, error) {
	return c.forwardCDN(ctx, MessageForwardCDNVideo, "video", request)
}

// ForwardCDNFile 使用宿主 CDN 资源引用投递文件。
func (c *MessageClient) ForwardCDNFile(ctx context.Context, request MessageCDNForwardRequest) (json.RawMessage, error) {
	return c.forwardCDN(ctx, MessageForwardCDNFile, "file", request)
}

func (c *MessageClient) forwardCDN(ctx context.Context, method Method, kind string, request MessageCDNForwardRequest) (json.RawMessage, error) {
	if request.Receiver == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, operation(method), "receiver is required")
	}
	if err := validateResourceRef(method, request.Ref, kind); err != nil {
		return nil, err
	}
	return (*Client)(c).call(ctx, method, request)
}
