package plugin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// mediaClientFixture 返回可观察的客户端流并保存调用上下文
type mediaClientFixture struct {
	wire.HostMessagesServiceClient

	ctx    context.Context           // SDK 创建的流上下文
	stream *mediaClientStreamFixture // 当前媒体流
}

// SendMedia 返回人工客户端流
func (f *mediaClientFixture) SendMedia(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[wire.SendMediaRequest, wire.SendReceiptResponse], error) {
	f.ctx = ctx
	return f.stream, nil
}

// mediaClientStreamFixture 记录发送帧并模拟服务端最终状态
type mediaClientStreamFixture struct {
	grpc.ClientStream

	requests     []*wire.SendMediaRequest  // 已发送的元数据及字节帧
	sendErrorAt  int                       // 指定从第几次 Send 开始失败，0 表示不失败
	sendErr      error                     // 模拟的发送失败
	closeAndRecv int                       // CloseAndRecv 调用次数
	response     *wire.SendReceiptResponse // 人工服务端回执
	responseErr  error                     // 人工服务端传输错误
}

// Send 保存成功发送的帧或模拟发送方向提前结束
func (f *mediaClientStreamFixture) Send(request *wire.SendMediaRequest) error {
	if f.sendErrorAt > 0 && len(f.requests)+1 >= f.sendErrorAt {
		return f.sendErr
	}
	f.requests = append(f.requests, proto.Clone(request).(*wire.SendMediaRequest))
	return nil
}

// CloseAndRecv 返回人工服务端结果
func (f *mediaClientStreamFixture) CloseAndRecv() (*wire.SendReceiptResponse, error) {
	f.closeAndRecv++
	return f.response, f.responseErr
}

// TestRemoteSendMediaChunksAndReceipt 验证多块发送、首帧顺序和成功回执
func TestRemoteSendMediaChunksAndReceipt(t *testing.T) {
	data := bytes.Repeat([]byte("m"), transport.MaxMediaChunkSize+17)
	for index := range data {
		data[index] = byte(index % 251)
	}
	receipt, conversion := wire.ToWireSendReceipt(&message.SendReceipt{
		MessageID:    "media-id",
		Conversation: sdk.Conversation{AccountUsername: "bot", Kind: sdk.ConversationKindDirect, ID: "friend"},
		SentAt:       time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), Outcome: sdk.OutcomeSucceeded,
	})
	if conversion != nil {
		t.Fatal(conversion)
	}
	stream := &mediaClientStreamFixture{response: &wire.SendReceiptResponse{Receipt: receipt}}
	client := &mediaClientFixture{stream: stream}
	messages := &remoteMessages{client: client}
	value, err := messages.SendMedia(t.Context(), sdk.Conversation{}, message.MediaUpload{
		Kind: sdk.MediaKindFile, FileName: "fixture.bin", Size: int64(len(data)), Reader: bytes.NewReader(data),
	})
	if err != nil || value == nil || value.MessageID != "media-id" || stream.closeAndRecv != 1 {
		t.Fatalf("媒体上传失败: receipt=%+v err=%v", value, err)
	}
	if len(stream.requests) != 3 || stream.requests[0].GetMetadata() == nil {
		t.Fatalf("媒体帧顺序不正确: %d", len(stream.requests))
	}
	var total int
	for _, request := range stream.requests[1:] {
		chunk := request.GetData()
		if len(chunk) == 0 || len(chunk) > transport.MaxMediaChunkSize {
			t.Fatalf("媒体分块越界: %d", len(chunk))
		}
		if !bytes.Equal(chunk, data[total:total+len(chunk)]) {
			t.Fatal("分块内容或顺序改变")
		}
		total += len(chunk)
	}
	if total != len(data) {
		t.Fatalf("媒体字节丢失: got=%d want=%d", total, len(data))
	}
}

// TestRemoteSendMediaReferenceDoesNotUploadBytes 验证引用发送只传首包且不读取插件字节
func TestRemoteSendMediaReferenceDoesNotUploadBytes(t *testing.T) {
	conversation := sdk.Conversation{AccountUsername: "bot", Kind: sdk.ConversationKindDirect, ID: "friend"}
	receipt, err := wire.ToWireSendReceipt(&message.SendReceipt{
		MessageID: "forwarded-media", Conversation: conversation,
		SentAt: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), Outcome: sdk.OutcomeSucceeded,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := &mediaClientStreamFixture{response: &wire.SendReceiptResponse{Receipt: receipt}}
	messages := &remoteMessages{client: &mediaClientFixture{stream: stream}}
	ref := &sdk.MediaRef{
		Token: "opaque-ref", Kind: sdk.MediaKindImage, FileName: "result.png",
		SizeBytes: 3, ExpiresAt: time.Now().Add(time.Minute),
	}
	value, err := messages.SendMedia(t.Context(), conversation, message.MediaUpload{Kind: sdk.MediaKindImage, Ref: ref})
	if err != nil || value == nil || value.MessageID != "forwarded-media" {
		t.Fatalf("引用发送失败: receipt=%+v err=%v", value, err)
	}
	if len(stream.requests) != 1 || stream.requests[0].GetMetadata().GetRef().GetToken() != ref.Token ||
		len(stream.requests[0].GetData()) != 0 {
		t.Fatalf("引用发送产生了字节帧: %+v", stream.requests)
	}
}

// TestRemoteSendMediaPreservesServerStatus 验证发送方向提前结束后仍读取服务端最终错误
func TestRemoteSendMediaPreservesServerStatus(t *testing.T) {
	business := sdk.NewError(sdk.CategoryUnavailable, "messages.send_media", "fixture rejected")
	business.Outcome = sdk.OutcomeUnknown
	stream := &mediaClientStreamFixture{
		sendErrorAt: 2,
		sendErr:     io.EOF,
		response:    &wire.SendReceiptResponse{Error: wire.ToWireError(business)},
	}
	client := &mediaClientFixture{stream: stream}
	messages := &remoteMessages{client: client}
	_, err := messages.SendMedia(t.Context(), sdk.Conversation{}, message.MediaUpload{
		Kind: sdk.MediaKindFile, FileName: "fixture.bin", Size: -1, Reader: bytes.NewReader([]byte("partial")),
	})
	var value *sdk.Error
	if !errors.As(err, &value) || value.Category != sdk.CategoryUnavailable || value.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("服务端状态丢失: %v", err)
	}
	if stream.closeAndRecv != 1 {
		t.Fatalf("未读取服务端最终状态: %d", stream.closeAndRecv)
	}
}

// TestRemoteSendMediaReaderFailureCancelsStream 验证来源错误会取消整个 RPC，避免半截媒体被当作成功
func TestRemoteSendMediaReaderFailureCancelsStream(t *testing.T) {
	sentinel := errors.New("fixture read failure")
	stream := &mediaClientStreamFixture{}
	client := &mediaClientFixture{stream: stream}
	messages := &remoteMessages{client: client}
	reader := &faultReader{data: []byte("partial"), err: sentinel}
	_, err := messages.SendMedia(t.Context(), sdk.Conversation{}, message.MediaUpload{
		Kind: sdk.MediaKindFile, FileName: "fixture.bin", Size: -1, Reader: reader,
	})
	var value *sdk.Error
	if !errors.As(err, &value) || value.Category != sdk.CategoryInternal || value.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("来源错误类别不正确: %v", err)
	}
	if stream.closeAndRecv != 0 {
		t.Fatal("来源失败后仍尝试完成媒体写入")
	}
	select {
	case <-client.ctx.Done():
	default:
		t.Fatal("来源失败后未取消媒体 RPC")
	}
}

// TestRemoteSendMediaCancellationStopsSend 验证读取期间取消不会继续发送媒体
func TestRemoteSendMediaCancellationStopsSend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reader := &cancelAfterReadReader{
		reader: bytes.NewReader(bytes.Repeat([]byte("c"), transport.MaxMediaChunkSize+1)),
		cancel: cancel,
	}
	stream := &mediaClientStreamFixture{}
	client := &mediaClientFixture{stream: stream}
	messages := &remoteMessages{client: client}
	_, err := messages.SendMedia(ctx, sdk.Conversation{}, message.MediaUpload{
		Kind: sdk.MediaKindFile, FileName: "fixture.bin", Size: -1, Reader: reader,
	})
	if !categoryOf(err, sdk.CategoryCanceled) {
		t.Fatalf("取消类别丢失: %v", err)
	}
	if stream.closeAndRecv != 0 {
		t.Fatal("取消后仍尝试完成媒体写入")
	}
	if len(stream.requests) != 1 || stream.requests[0].GetMetadata() == nil {
		t.Fatalf("取消后仍发送了媒体块: %d", len(stream.requests))
	}
}

// faultReader 返回一段数据后模拟来源故障
type faultReader struct {
	data []byte // 故障前可读取的人工数据
	err  error  // 模拟的来源错误
	read bool   // 是否已返回过数据
}

// Read 首次返回数据，后续返回来源错误
func (r *faultReader) Read(target []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	return copy(target, r.data), nil
}

// cancelAfterReadReader 在首次读取后触发调用取消
type cancelAfterReadReader struct {
	reader io.Reader          // 人工媒体来源
	cancel context.CancelFunc // 模拟调用方取消
}

// Read 读取一段媒体并立即取消上下文
func (r *cancelAfterReadReader) Read(target []byte) (int, error) {
	n, err := r.reader.Read(target)
	r.cancel()
	return n, err
}
