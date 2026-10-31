package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/message"
	"google.golang.org/grpc"
)

// backgroundCallerClient 记录 SDK 选择的后台 RPC 及启用令牌
type backgroundCallerClient struct {
	wire.HostCallerServiceClient

	request *wire.HostBackgroundCallRequest
}

// backgroundMessagesClient 记录后台文本发送请求
type backgroundMessagesClient struct {
	wire.HostMessagesServiceClient

	request *wire.SendTextRequest
}

// backgroundAccountClient 记录后台账号查询请求
type backgroundAccountClient struct {
	wire.HostAccountServiceClient

	request *wire.AccountCurrentRequest
}

// Current 返回只含业务字段的当前账号快照
func (c *backgroundAccountClient) Current(_ context.Context, request *wire.AccountCurrentRequest, _ ...grpc.CallOption) (*wire.AccountResponse, error) {
	c.request = request
	account, err := wire.ToWireAccount(&sdk.Account{Username: "account", Online: true, Status: sdk.AccountStatusOnline, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	return &wire.AccountResponse{Account: account}, nil
}

// SendText 返回完整成功回执，证明后台令牌经过同一消息 RPC
func (c *backgroundMessagesClient) SendText(_ context.Context, request *wire.SendTextRequest, _ ...grpc.CallOption) (*wire.SendReceiptResponse, error) {
	c.request = request
	receipt, err := wire.ToWireSendReceipt(&message.SendReceipt{
		MessageID: "background", Conversation: wire.FromWireConversation(request.GetConversation()),
		SentAt: time.Now().UTC(), Outcome: sdk.OutcomeSucceeded,
	})
	if err != nil {
		return nil, err
	}
	return &wire.SendReceiptResponse{Receipt: receipt}, nil
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
	if _, err := host.Caller.Call(nil, request); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("空后台调用上下文未拒绝: %v", err)
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

// TestRemoteMessagesBackgroundContext 验证后台文本只从当前启用周期取得令牌
func TestRemoteMessagesBackgroundContext(t *testing.T) {
	client := &backgroundMessagesClient{}
	host := &Host{}
	messages := &remoteMessages{client: client, host: host}
	target := sdk.Conversation{AccountUsername: "account", Kind: sdk.ConversationKindDirect, ID: "alice"}
	if _, err := messages.SendText(context.Background(), target, "denied", nil); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("无启用周期的后台文本未拒绝: %v", err)
	}
	if _, err := messages.SendText(nil, target, "nil-context", nil); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("空后台文本上下文未拒绝: %v", err)
	}
	cancel := host.startTasks(t.Context(), "background-message-token")
	if _, err := messages.SendText(host.TaskContext(), target, "scheduled", nil); err != nil {
		t.Fatal(err)
	}
	if client.request.GetBackgroundToken() != "background-message-token" ||
		client.request.GetInvocation().GetId() != "" || wire.FromWireConversation(client.request.GetConversation()) != target {
		t.Fatalf("后台文本请求=%+v", client.request)
	}
	ordinary := transport.WithInvocation(host.TaskContext(), "ordinary-id", "ordinary-token")
	if _, err := messages.SendText(ordinary, target, "ordinary", nil); err != nil {
		t.Fatal(err)
	}
	if client.request.GetBackgroundToken() != "" || client.request.GetInvocation().GetId() != "ordinary-id" ||
		client.request.GetInvocation().GetToken() != "ordinary-token" {
		t.Fatalf("普通文本请求混入后台令牌=%+v", client.request)
	}
	cancel()
	if _, err := messages.SendText(host.TaskContext(), target, "stale", nil); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("取消后的后台文本未拒绝: %v", err)
	}
	host.startTasks(t.Context(), "next-background-token")
	if _, err := messages.SendText(host.TaskContext(), target, "next", nil); err != nil || client.request.GetBackgroundToken() != "next-background-token" {
		t.Fatalf("重启用后台文本令牌=%q err=%v", client.request.GetBackgroundToken(), err)
	}
}

// TestRemoteAccountBackgroundContext 验证后台账号查询只从当前启用周期取得令牌
func TestRemoteAccountBackgroundContext(t *testing.T) {
	client := &backgroundAccountClient{}
	host := &Host{}
	account := &remoteAccount{client: client, host: host}
	if _, err := account.Current(context.Background()); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("无启用周期的后台账号查询未拒绝: %v", err)
	}
	if _, err := account.Current(nil); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("空后台账号上下文未拒绝: %v", err)
	}
	cancel := host.startTasks(t.Context(), "background-account-token")
	value, err := account.Current(host.TaskContext())
	if err != nil || value == nil || value.Username != "account" {
		t.Fatalf("后台账号查询结果=%+v err=%v", value, err)
	}
	if client.request.GetBackgroundToken() != "background-account-token" || client.request.GetInvocation().GetId() != "" {
		t.Fatalf("后台账号查询请求=%+v", client.request)
	}
	ordinary := transport.WithInvocation(host.TaskContext(), "ordinary-account", "ordinary-token")
	if _, err := account.Current(ordinary); err != nil {
		t.Fatal(err)
	}
	if client.request.GetBackgroundToken() != "" || client.request.GetInvocation().GetId() != "ordinary-account" {
		t.Fatalf("普通账号查询混入后台令牌=%+v", client.request)
	}
	cancel()
	if _, err := account.Current(host.TaskContext()); !categoryOf(err, sdk.CategoryPermissionDenied) {
		t.Fatalf("取消后的后台账号查询未拒绝: %v", err)
	}
	host.startTasks(t.Context(), "next-account-token")
	if _, err := account.Current(host.TaskContext()); err != nil || client.request.GetBackgroundToken() != "next-account-token" {
		t.Fatalf("重启用后台账号令牌=%q err=%v", client.request.GetBackgroundToken(), err)
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
