package plugin

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// lifecycleFixture 观察取消后的排队控制操作是否仍进入用户代码
type lifecycleFixture struct {
	// enabled 已实际进入的启用调用数
	enabled atomic.Int32
}

// Descriptor 提供测试固定声明
func (*lifecycleFixture) Descriptor() Descriptor { return Descriptor{ID: "fixture"} }

// OnEnable 登记实际执行次数
func (p *lifecycleFixture) OnEnable(context.Context) error { p.enabled.Add(1); return nil }

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
