package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
)

// backgroundCallerClient 记录 SDK 选择的后台 RPC 及启用令牌
type backgroundCallerClient struct {
	wire.HostCallerServiceClient

	request *wire.HostBackgroundCallRequest
}

// Call 普通业务调用不应出现在后台测试
func (c *backgroundCallerClient) Call(context.Context, *wire.HostCallRequest, ...grpc.CallOption) (*wire.HostCallResponse, error) {
	return nil, sdk.NewError(sdk.CategoryInternal, "fixture.call", "unexpected ordinary call")
}

// CallBackground 返回与请求相同的有效能力结果
func (c *backgroundCallerClient) CallBackground(_ context.Context, request *wire.HostBackgroundCallRequest, _ ...grpc.CallOption) (*wire.HostCallResponse, error) {
	c.request = request
	return &wire.HostCallResponse{Result: &wire.CallResult{ContentType: "application/json", Payload: &wire.BytesValue{Value: []byte("{}")}}}, nil
}

// TestRemoteCallerBackgroundContext 验证仅当前 TaskContext 派生上下文可以发起后台调用
func TestRemoteCallerBackgroundContext(t *testing.T) {
	client := &backgroundCallerClient{}
	host := &Host{}
	host.Caller = &remoteCaller{client: client, host: host}
	request := capability.CallRequest{CapabilityName: "fixture.echo", ContentType: "application/json", Payload: []byte("{}")}
	target := sdk.Conversation{AccountUsername: "account", Kind: sdk.ConversationKindDirect, ID: "alice"}
	if _, err := host.Caller.Call(context.Background(), request); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("无目标后台调用未拒绝: %v", err)
	}
	if _, err := host.Caller.Call(capability.WithBackgroundTarget(context.Background(), target), request); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("非 TaskContext 后台调用未拒绝: %v", err)
	}
	if _, err := host.Caller.Call(capability.WithBackgroundTarget(context.Background(), target), capability.CallRequest{Payload: make([]byte, transport.MaxInlinePayloadSize+1)}); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("非 TaskContext 后台超限请求先泄露资源限制: %v", err)
	}

	cancel := host.startTasks(t.Context(), "background-token-1")
	first := host.TaskContext()
	ctx := capability.WithBackgroundTarget(first, target)
	detached := capability.WithBackgroundTarget(context.WithoutCancel(first), target)
	result, err := host.Caller.Call(ctx, request)
	if err != nil || result == nil || client.request.GetBackgroundToken() != "background-token-1" || wire.FromWireConversation(client.request.GetConversation()) != target {
		t.Fatalf("有效后台调用或独立目标丢失: %+v %+v %v", result, client.request, err)
	}
	cancel()
	for _, stale := range []context.Context{ctx, detached} {
		if _, err := host.Caller.Call(stale, request); !categoryOf(err, sdk.CategoryPermissionDenied) {
			t.Fatalf("已取消周期或取消剥离上下文仍可后台调用: %v", err)
		}
	}

	cancel = host.startTasks(t.Context(), "background-token-2")
	defer cancel()
	for _, stale := range []context.Context{ctx, detached} {
		if _, err := host.Caller.Call(stale, request); !categoryOf(err, sdk.CategoryPermissionDenied) {
			t.Fatalf("重启用后旧周期或取消剥离上下文仍可后台调用: %v", err)
		}
	}
	ctx = capability.WithBackgroundTarget(host.TaskContext(), target)
	if _, err := host.Caller.Call(ctx, request); err != nil || client.request.GetBackgroundToken() != "background-token-2" {
		t.Fatalf("重启用未签发新令牌: %+v %v", client.request, err)
	}
}

// TestRemoteCallerRejectsOversizedAndUnknownTransport 验证内联上限及未知传输结果
func TestRemoteCallerRejectsOversizedAndUnknownTransport(t *testing.T) {
	caller := &remoteCaller{client: &backgroundCallerClient{}, host: &Host{}}
	_, err := caller.Call(t.Context(), capability.CallRequest{Payload: make([]byte, transport.MaxInlinePayloadSize+1)})
	if !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("无身份超限载荷先泄露资源限制: %v", err)
	}
	_, err = caller.Call(transport.WithInvocation(t.Context(), "id", ""), capability.CallRequest{Payload: make([]byte, transport.MaxInlinePayloadSize+1)})
	if !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("无用户凭据的超限载荷先泄露资源限制: %v", err)
	}
	_, err = caller.Call(transport.WithInvocation(t.Context(), "id", "token"), capability.CallRequest{Payload: make([]byte, transport.MaxInlinePayloadSize+1)})
	if !categoryOf(err, sdk.CategoryResourceExhausted) {
		t.Fatalf("超限载荷错误类别不正确: %v", err)
	}
	ctx := transport.WithInvocation(t.Context(), "id", "token")
	_, err = caller.Call(ctx, capability.CallRequest{CapabilityName: "fixture.echo"})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("能力传输错误未标记结果未知: %v", err)
	}
}

// categoryOf 避免在客户端测试暴露内部传输错误实现
func categoryOf(err error, category sdk.Category) bool {
	var value *sdk.Error
	return errors.As(err, &value) && value.Category == category
}
