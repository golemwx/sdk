package rpc

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// mediaServerClientFixture 以人工业务入口观察宿主媒体接收
type mediaServerClientFixture struct {
	plugin.MessagesClient

	calls int                             // 进入宿主媒体业务的次数
	run   func(message.MediaUpload) error // 模拟宿主对媒体流的处理
}

// SendMedia 读取或拒绝宿主提供的 pipe 并返回人工结果
func (f *mediaServerClientFixture) SendMedia(ctx context.Context, conversation sdk.Conversation, upload message.MediaUpload) (*message.SendReceipt, error) {
	f.calls++
	if f.run != nil {
		if err := f.run(upload); err != nil {
			return nil, err
		}
	}
	return &message.SendReceipt{
		MessageID: "media-id", Conversation: conversation, SentAt: fixtureTime(), Outcome: sdk.OutcomeSucceeded,
	}, nil
}

// mediaServerStreamFixture 按固定脚本回放客户端媒体帧
type mediaServerStreamFixture struct {
	grpc.ServerStream

	ctx      context.Context           // 流上下文
	requests []*wire.SendMediaRequest  // 待回放的客户端帧
	position int                       // 下一帧位置
	wait     bool                      // 脚本耗尽后等待上下文取消，模拟实例退出
	response *wire.SendReceiptResponse // SendAndClose 捕获的最终响应
}

// Context 返回人工流上下文
func (s *mediaServerStreamFixture) Context() context.Context { return s.ctx }

// Recv 按脚本返回下一帧，脚本结束后返回 EOF
func (s *mediaServerStreamFixture) Recv() (*wire.SendMediaRequest, error) {
	if s.position >= len(s.requests) {
		if s.wait {
			<-s.ctx.Done()
			return nil, s.ctx.Err()
		}
		return nil, io.EOF
	}
	request := s.requests[s.position]
	s.position++
	return request, nil
}

// SendAndClose 保存宿主最终响应
func (s *mediaServerStreamFixture) SendAndClose(response *wire.SendReceiptResponse) error {
	s.response = response
	return nil
}

// TestHostSendMediaRejectsOversizedChunk 验证超限分块不会进入宿主业务
func TestHostSendMediaRejectsOversizedChunk(t *testing.T) {
	client := &mediaServerClientFixture{}
	server := &hostMessages{client: client}
	stream := &mediaServerStreamFixture{ctx: t.Context(), requests: []*wire.SendMediaRequest{
		{Value: &wire.SendMediaRequest_Metadata{Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", -1)}},
		{Value: &wire.SendMediaRequest_Data{Data: make([]byte, transport.MaxMediaChunkSize+1)}},
	}}
	err := server.SendMedia(stream)
	if !hasCategory(transport.DecodeError(nil, err, "messages.send_media"), sdk.CategoryInvalidArgument) {
		t.Fatalf("超限分块错误类别不正确: %v", err)
	}
	if client.calls != 0 {
		t.Fatal("超限分块进入了宿主业务")
	}
}

// TestHostSendMediaRejectsDeclaredLengthMismatch 验证接收校验错误优先于 pipe 关闭引发的读取失败
func TestHostSendMediaRejectsDeclaredLengthMismatch(t *testing.T) {
	client := &mediaServerClientFixture{run: func(upload message.MediaUpload) error {
		_, _ = io.ReadAll(upload.Reader)
		return errors.New("fixture core wrapped read failure")
	}}
	server := &hostMessages{client: client}
	stream := &mediaServerStreamFixture{ctx: t.Context(), requests: []*wire.SendMediaRequest{
		{Value: &wire.SendMediaRequest_Metadata{Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", 3)}},
		{Value: &wire.SendMediaRequest_Data{Data: []byte("too-long")}},
	}}
	if err := server.SendMedia(stream); err != nil {
		t.Fatal(err)
	}
	if stream.response.GetError().GetCategory() != wire.Category_CATEGORY_INVALID_ARGUMENT ||
		stream.response.GetError().GetOutcome() != wire.WriteOutcome_WRITE_OUTCOME_UNKNOWN || client.calls != 1 {
		t.Fatalf("声明长度错误未保留: %+v", stream.response)
	}
}

// TestHostSendMediaRejectsMalformedContinuation 验证短读、后续空块和重复元数据不会得到成功回执
func TestHostSendMediaRejectsMalformedContinuation(t *testing.T) {
	for _, test := range []struct {
		name string                   // 非法续帧场景
		next []*wire.SendMediaRequest // 首个数据块之后的帧
	}{
		{name: "short_read"},
		{name: "empty_chunk", next: []*wire.SendMediaRequest{{Value: &wire.SendMediaRequest_Data{}}}},
		{name: "repeated_metadata", next: []*wire.SendMediaRequest{{Value: &wire.SendMediaRequest_Metadata{
			Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", 2),
		}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &mediaServerClientFixture{run: func(upload message.MediaUpload) error {
				_, err := io.Copy(io.Discard, upload.Reader)
				return err
			}}
			requests := []*wire.SendMediaRequest{
				{Value: &wire.SendMediaRequest_Metadata{Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", 2)}},
				{Value: &wire.SendMediaRequest_Data{Data: []byte("x")}},
			}
			stream := &mediaServerStreamFixture{ctx: t.Context(), requests: append(requests, test.next...)}
			if err := (&hostMessages{client: client}).SendMedia(stream); err != nil {
				t.Fatal(err)
			}
			if stream.response.GetReceipt() != nil || stream.response.GetError().GetCategory() != wire.Category_CATEGORY_INVALID_ARGUMENT {
				t.Fatalf("非法媒体续帧未被拒绝: %+v", stream.response)
			}
		})
	}
}

// TestHostSendMediaCoreFailureDoesNotWaitForFurtherInput 验证宿主失败后不等待后续客户端帧
func TestHostSendMediaCoreFailureDoesNotWaitForFurtherInput(t *testing.T) {
	business := sdk.NewError(sdk.CategoryUnavailable, "fixture.media", "core unavailable")
	client := &mediaServerClientFixture{run: func(message.MediaUpload) error { return business }}
	server := &hostMessages{client: client}
	stream := &mediaServerStreamFixture{ctx: t.Context(), requests: []*wire.SendMediaRequest{
		{Value: &wire.SendMediaRequest_Metadata{Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", -1)}},
		{Value: &wire.SendMediaRequest_Data{Data: []byte("first")}},
		// 缺少终止帧，宿主业务提前失败时不应等待该帧
	}}
	if err := server.SendMedia(stream); err != nil {
		t.Fatal(err)
	}
	if stream.response.GetError().GetCategory() != wire.Category_CATEGORY_UNAVAILABLE {
		t.Fatalf("宿主失败类别丢失: %+v", stream.response)
	}
}

// TestHostSendMediaInstanceExitCancelsReader 验证插件实例退出导致流取消时，
// 宿主读取会结束并由接收侧关闭 pipe，不残留阻塞读取
func TestHostSendMediaInstanceExitCancelsReader(t *testing.T) {
	entered := make(chan struct{})
	client := &mediaServerClientFixture{run: func(upload message.MediaUpload) error {
		close(entered)
		_, err := io.ReadAll(upload.Reader)
		return err
	}}
	ctx, cancel := context.WithCancel(t.Context())
	stream := &mediaServerStreamFixture{
		ctx: ctx,
		requests: []*wire.SendMediaRequest{
			{Value: &wire.SendMediaRequest_Metadata{Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", -1)}},
			{Value: &wire.SendMediaRequest_Data{Data: []byte("first")}},
		},
		wait: true,
	}
	server := &hostMessages{client: client}
	done := make(chan error, 1)
	go func() { done <- server.SendMedia(stream) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("宿主业务未开始读取媒体")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("实例退出后媒体流未结束")
	}
	if stream.response.GetError().GetCategory() != wire.Category_CATEGORY_CANCELED ||
		stream.response.GetError().GetOutcome() != wire.WriteOutcome_WRITE_OUTCOME_UNKNOWN {
		t.Fatalf("实例退出错误未保留: %+v", stream.response)
	}
}

// fixtureMediaMetadata 构造合法的人工媒体首帧
func fixtureMediaMetadata(kind sdk.MediaKind, name string, size int64) *wire.SendMediaMetadata {
	return &wire.SendMediaMetadata{Kind: wire.MediaKind(kind), FileName: name, Size: size}
}

// pendingMediaStream 模拟客户端保持连接但停止提供媒体分块
type pendingMediaStream struct {
	grpc.ServerStream
	ctx      context.Context           // 原始连接的上下文
	requests []*wire.SendMediaRequest  // 阻塞前已到达的帧
	pending  chan struct{}             // 已开始等待下一帧
	response *wire.SendReceiptResponse // 宿主最终响应
}

// Context 保持原始连接有效，以区别于宿主授权取消
func (s *pendingMediaStream) Context() context.Context { return s.ctx }

// RecvMsg 回放帧后等待原始连接结束，模拟 gRPC 底层读取
func (s *pendingMediaStream) RecvMsg(target any) error {
	if len(s.requests) > 0 {
		proto.Merge(target.(proto.Message), s.requests[0])
		s.requests = s.requests[1:]
		return nil
	}
	close(s.pending)
	<-s.ctx.Done()
	return s.ctx.Err()
}

// SendMsg 保存类型化 handler 的最终响应
func (s *pendingMediaStream) SendMsg(value any) error {
	s.response = value.(*wire.SendReceiptResponse)
	return nil
}

// TestHostSendMediaAuthorizationCancellation 验证只有宿主授权取消时也能解除首块及后续块等待
func TestHostSendMediaAuthorizationCancellation(t *testing.T) {
	for _, withData := range []bool{false, true} {
		name := "first_chunk"
		if withData {
			name = "following_chunk"
		}
		t.Run(name, func(t *testing.T) {
			connectionCtx, disconnect := context.WithCancel(t.Context())
			defer disconnect()
			authorityCtx, revoke := context.WithCancel(t.Context())
			defer revoke()
			stream := &pendingMediaStream{
				ctx: connectionCtx, pending: make(chan struct{}),
				requests: []*wire.SendMediaRequest{{Value: &wire.SendMediaRequest_Metadata{
					Metadata: fixtureMediaMetadata(sdk.MediaKindFile, "fixture.bin", -1),
				}}},
			}
			if withData {
				stream.requests = append(stream.requests, &wire.SendMediaRequest{Value: &wire.SendMediaRequest_Data{Data: []byte("first")}})
			}
			client := &mediaServerClientFixture{run: func(upload message.MediaUpload) error {
				_, err := io.Copy(io.Discard, upload.Reader)
				return err
			}}
			host := &hostMessages{client: client}
			interceptor := hostStreamInterceptor(HostServices{Authorize: func(context.Context, Invocation, string) (context.Context, error) {
				return authorityCtx, nil
			}}, nil)
			done := make(chan error, 1)
			go func() {
				done <- interceptor(host, stream, &grpc.StreamServerInfo{FullMethod: wire.HostMessagesService_SendMedia_FullMethodName},
					func(_ any, authorized grpc.ServerStream) error {
						return host.SendMedia(&grpc.GenericServerStream[wire.SendMediaRequest, wire.SendReceiptResponse]{ServerStream: authorized})
					})
			}()
			select {
			case <-stream.pending:
			case <-time.After(time.Second):
				t.Fatal("媒体流未开始等待客户端分块")
			}
			revoke()
			select {
			case err := <-done:
				if err != nil && !hasCategory(transport.DecodeError(nil, err, "messages.send_media"), sdk.CategoryCanceled) {
					t.Fatalf("授权取消类别丢失: %v", err)
				}
				if err == nil && stream.response.GetError().GetCategory() != wire.Category_CATEGORY_CANCELED {
					t.Fatalf("取消未保留到媒体响应: %+v", stream.response)
				}
			case <-time.After(time.Second):
				t.Fatal("授权取消后仍在等待客户端分块")
			}
		})
	}
}
