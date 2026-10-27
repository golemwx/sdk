package plugin

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// lifecycleFixture 观察取消后的排队控制操作是否仍进入用户代码
type lifecycleFixture struct {
	enabled    atomic.Int32          // 已实际进入的启用调用数
	invocation chan *wire.Invocation // 可选的生命周期凭据观察通道
}

// Descriptor 提供测试固定声明
func (*lifecycleFixture) Descriptor() Descriptor { return Descriptor{ID: "fixture"} }

// OnEnable 登记实际执行次数
func (p *lifecycleFixture) OnEnable(ctx context.Context) error {
	p.enabled.Add(1)
	if p.invocation != nil {
		p.invocation <- transport.InvocationFromContext(ctx)
	}
	return nil
}

// lifecycleIncomingContext 模拟只有安全正向 RPC 才会提供的宿主凭据 metadata
func lifecycleIncomingContext(ctx context.Context, id, token string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(
		transport.LifecycleInvocationIDHeader, id,
		transport.LifecycleInvocationTokenHeader, token,
	))
}

// TestCanceledQueuedLifecycle 不允许已经取消的排队控制操作执行副作用
func TestCanceledQueuedLifecycle(t *testing.T) {
	implementation := &lifecycleFixture{}
	server := &controlServer{implementation: implementation, conn: &grpc.ClientConn{}, loaded: true}
	server.mu.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan *wire.OperationResponse, 1)
	go func() {
		close(started)
		response, _ := server.OnEnable(ctx, &wire.LifecycleRequest{})
		finished <- response
	}()
	<-started
	cancel()
	server.mu.Unlock()
	response := <-finished
	if response.GetError().GetCategory() != wire.Category_CATEGORY_CANCELED || implementation.enabled.Load() != 0 || server.enabled {
		t.Fatal("canceled lifecycle executed plugin code")
	}
}

// TestLifecycleRequiresHostCredential 验证插件不能使用空凭据或篡改请求标识伪造生命周期身份
func TestLifecycleRequiresHostCredential(t *testing.T) {
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing", ctx: t.Context()},
		{name: "mismatched", ctx: lifecycleIncomingContext(t.Context(), "other", "token")},
		{name: "empty-token", ctx: lifecycleIncomingContext(t.Context(), "enable", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			implementation := &lifecycleFixture{}
			server, _ := newExecutionFixture(t, implementation)
			server.enabled, server.accepting = false, false
			response, _ := server.OnEnable(test.ctx, &wire.LifecycleRequest{InvocationId: "enable"})
			if response.GetError().GetCategory() != wire.Category_CATEGORY_PERMISSION_DENIED || implementation.enabled.Load() != 0 {
				t.Fatalf("untrusted lifecycle reached plugin: %v", response)
			}
		})
	}
}

// TestLifecycleBindsHostCredential 验证可信 metadata 被绑定到插件生命周期 context
func TestLifecycleBindsHostCredential(t *testing.T) {
	implementation := &lifecycleFixture{invocation: make(chan *wire.Invocation, 1)}
	server, _ := newExecutionFixture(t, implementation)
	server.enabled, server.accepting = false, false
	response, _ := server.OnEnable(lifecycleIncomingContext(t.Context(), "enable", "host-token"), &wire.LifecycleRequest{InvocationId: "enable"})
	invocation := <-implementation.invocation
	if response.GetError() != nil || invocation.GetId() != "enable" || invocation.GetToken() != "host-token" {
		t.Fatalf("lifecycle credential was not bound: %v %v", response, invocation)
	}
}

// TestReceiptResultPreservesPartialError 验证业务错误旁的有效回执不会被丢弃
func TestReceiptResultPreservesPartialError(t *testing.T) {
	response := &wire.SendReceiptResponse{Receipt: &wire.SendReceipt{MessageId: "18446744073709551615", SentAt: &timestamppb.Timestamp{Seconds: 1}}, Error: wire.ToWireError(&sdk.Error{Category: sdk.CategoryDeadlineExceeded, Outcome: sdk.OutcomeUnknown})}
	receipt, err := receiptResult(response, nil, "messages.send_text")
	if err == nil || receipt == nil || receipt.MessageID != response.Receipt.MessageId {
		t.Fatal("partial receipt lost")
	}
}

// TestMalformedReceiptKeepsUnknownOutcome 验证无法解析的发送回执仍标记可能已经生效
func TestMalformedReceiptKeepsUnknownOutcome(t *testing.T) {
	receipt, err := receiptResult(&wire.SendReceiptResponse{Receipt: &wire.SendReceipt{}}, nil, "messages.send_text")
	value, ok := err.(*sdk.Error)
	if receipt != nil || !ok || value.Category != sdk.CategoryInternal || value.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("invalid write projection: %v", err)
	}
}
