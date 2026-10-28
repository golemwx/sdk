package rpc

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// backgroundHostCaller 检查业务适配只传递独立后台目标
type backgroundHostCaller struct {
	mu     sync.Mutex
	target sdk.Conversation
	calls  int
}

// Call 普通调用不属于本测试的后台入口
func (*backgroundHostCaller) Call(context.Context, capability.CallRequest) (*capability.CallResult, error) {
	return nil, sdk.NewError(sdk.CategoryPermissionDenied, "fixture.call", "unexpected ordinary call")
}

// CallBackground 保留实际收到的目标，不伪造用户上下文
func (c *backgroundHostCaller) CallBackground(ctx context.Context, target sdk.Conversation, request capability.CallRequest) (*capability.CallResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.target, c.calls = target, c.calls+1
	if value, ok := BackgroundConversation(ctx); !ok || value != target {
		return nil, sdk.NewError(sdk.CategoryInternal, "fixture.call", "background target lost")
	}
	return &capability.CallResult{ContentType: request.ContentType, Payload: request.Payload}, nil
}

// TestBackgroundRPCBoundary 验证无用户 token 的后台入口必须携带启用令牌
func TestBackgroundRPCBoundary(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	caller := &backgroundHostCaller{}
	var authorized string
	server := grpc.NewServer(grpc.UnaryInterceptor(hostInterceptor(HostServices{Host: &plugin.Host{Caller: caller}, Authorize: func(ctx context.Context, invocation Invocation, method string) (context.Context, error) {
		if method != wire.HostCallerService_CallBackground_FullMethodName || invocation.Token != "" {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "fixture.authorize", "invalid background identity")
		}
		token, ok := BackgroundToken(ctx)
		if !ok {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "fixture.authorize", "missing background token")
		}
		authorized = token
		return ctx, nil
	}}, &hostControl{})))
	wire.RegisterHostCallerServiceServer(server, &hostCaller{client: caller})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })

	conn, err := grpc.NewClient("passthrough:///background", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	client := wire.NewHostCallerServiceClient(conn)
	target := &wire.Conversation{AccountUsername: "account", Kind: wire.ConversationKind_CONVERSATION_KIND_DIRECT, Id: "alice"}
	response, err := client.CallBackground(t.Context(), &wire.HostBackgroundCallRequest{Conversation: target, Request: &wire.CallRequest{CapabilityName: "fixture.echo"}})
	if !hasCategory(transport.DecodeError(response.GetError(), err, "caller.call"), sdk.CategoryPermissionDenied) {
		t.Fatalf("伪造后台请求未拒绝: %+v %v", response, err)
	}
	response, err = client.CallBackground(t.Context(), &wire.HostBackgroundCallRequest{BackgroundToken: "background-token", Conversation: target, Request: &wire.CallRequest{CapabilityName: "fixture.echo", ContentType: "application/json", Payload: &wire.BytesValue{Value: []byte("{}")}}})
	if err != nil || response.GetError() != nil || authorized != "background-token" {
		t.Fatalf("合法后台请求或令牌丢失: %+v %v token=%q", response, err, authorized)
	}
	caller.mu.Lock()
	defer caller.mu.Unlock()
	if caller.calls != 1 || caller.target != (sdk.Conversation{AccountUsername: "account", Kind: sdk.ConversationKindDirect, ID: "alice"}) {
		t.Fatalf("后台目标或调用次数错误: %+v calls=%d", caller.target, caller.calls)
	}
}
