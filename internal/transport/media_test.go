package transport

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

// TestValidateMediaUpload 验证类型、文件名、总量及语音参数的冻结边界
func TestValidateMediaUpload(t *testing.T) {
	valid := message.MediaUpload{Kind: sdk.MediaKindImage, FileName: "图片.png", Size: 1, Reader: strings.NewReader("x")}
	if err := ValidateMediaUpload(valid); err != nil {
		t.Fatal(err)
	}
	invalid := []message.MediaUpload{
		{Kind: sdk.MediaKindOther, FileName: "x.bin", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "../x.png", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "C:x.png", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "x.", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindFile, FileName: "CON", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindFile, FileName: "nul.txt", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "x.exe", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "x.png", Size: 0, Reader: strings.NewReader("")},
		{Kind: sdk.MediaKindEmoji, FileName: "x.gif", Size: MaxEmojiMediaSize + 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindVoice, FileName: "x.amr", Size: 1, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, FileName: "x.png", Size: 1, Reader: strings.NewReader("x"), Voice: &message.VoiceOptions{Duration: time.Second, Encoding: message.VoiceEncodingAMR}},
	}
	for index, upload := range invalid {
		if err := ValidateMediaUpload(upload); err == nil || !hasOutcome(err, sdk.OutcomeFailed) {
			t.Fatalf("invalid upload %d was accepted", index)
		}
	}
	voice := message.MediaUpload{Kind: sdk.MediaKindVoice, FileName: "x.amr", Size: -1, Reader: strings.NewReader("x"), Voice: &message.VoiceOptions{Duration: time.Second, Encoding: message.VoiceEncodingAMR}}
	if err := ValidateMediaUpload(voice); err != nil {
		t.Fatal(err)
	}
	ref := &sdk.MediaRef{
		Token: "opaque", Kind: sdk.MediaKindImage, FileName: "result.png",
		SizeBytes: 3, ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := ValidateMediaUpload(message.MediaUpload{Kind: sdk.MediaKindImage, Ref: ref}); err != nil {
		t.Fatal(err)
	}
	for _, upload := range []message.MediaUpload{
		{Kind: sdk.MediaKindFile, Ref: ref},
		{Kind: sdk.MediaKindImage, Ref: ref, Reader: strings.NewReader("x")},
		{Kind: sdk.MediaKindImage, Ref: &sdk.MediaRef{}},
	} {
		if err := ValidateMediaUpload(upload); err == nil {
			t.Fatalf("非法引用发送被接受: %+v", upload)
		}
	}
}

// TestValidateMediaTotal 验证超总量、短读、多读和空流均明确失败
func TestValidateMediaTotal(t *testing.T) {
	for _, test := range []struct {
		declared int64            // 元数据声明的字节数
		actual   int64            // 实际读取的字节数
		complete bool             // 是否已到达 EOF
		category sdk.Category     // 预期错误类别
		outcome  sdk.WriteOutcome // 预期写入结果
	}{
		{3, 3, true, sdk.CategoryUnknown, sdk.OutcomeUnspecified},
		{3, 2, true, sdk.CategoryInvalidArgument, sdk.OutcomeUnknown},
		{3, 4, false, sdk.CategoryInvalidArgument, sdk.OutcomeUnknown},
		{-1, 0, true, sdk.CategoryInvalidArgument, sdk.OutcomeFailed},
		{-1, MaxEmojiMediaSize + 1, false, sdk.CategoryResourceExhausted, sdk.OutcomeUnknown},
	} {
		err := ValidateMediaTotal(sdk.MediaKindEmoji, test.declared, test.actual, test.complete)
		if test.category == sdk.CategoryUnknown && err != nil {
			t.Fatal(err)
		}
		if test.category != sdk.CategoryUnknown {
			var value *sdk.Error
			if !errors.As(err, &value) || value.Category != test.category || value.Outcome != test.outcome {
				t.Fatalf("unexpected total error: %v", err)
			}
		}
	}
}

// hasOutcome 判断媒体错误保留了预期的写副作用结果
func hasOutcome(err error, outcome sdk.WriteOutcome) bool {
	var value *sdk.Error
	return errors.As(err, &value) && value.Outcome == outcome
}

// TestReadMediaChunk 验证短读和长期无进展 Reader 的处理保持有界
func TestReadMediaChunk(t *testing.T) {
	reader := bytes.NewBufferString("abc")
	buffer := make([]byte, 2)
	empty := 0
	if n, err := ReadMediaChunk(reader, buffer, &empty); err != nil || n != 2 || string(buffer[:n]) != "ab" {
		t.Fatalf("short chunk changed: n=%d err=%v", n, err)
	}
	for range 99 {
		if _, err := ReadMediaChunk(zeroReader{}, buffer, &empty); err != nil {
			t.Fatalf("no-progress rejected too early: %v", err)
		}
	}
	if _, err := ReadMediaChunk(zeroReader{}, buffer, &empty); !errors.Is(err, io.ErrNoProgress) {
		t.Fatalf("no-progress reader was not bounded: %v", err)
	}
}

// TestReadMediaChunkInvalidCount 验证异常 Reader 返回计数不会引发切片越界
func TestReadMediaChunkInvalidCount(t *testing.T) {
	for _, count := range []int{-1, 3} {
		empty := 0
		if n, err := ReadMediaChunk(invalidCountReader(count), make([]byte, 2), &empty); n != 0 || err == nil {
			t.Fatalf("异常读取计数未被拒绝: count=%d n=%d err=%v", count, n, err)
		}
	}
}

// invalidCountReader 模拟违反 io.Reader 计数约定的来源
type invalidCountReader int

// Read 返回指定异常计数而不访问缓冲
func (r invalidCountReader) Read([]byte) (int, error) { return int(r), nil }

// zeroReader 模拟违反 Reader 建议且永不推进的来源
type zeroReader struct{}

// Read 始终返回零字节和 nil
func (zeroReader) Read([]byte) (int, error) { return 0, nil }
