package plugin

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// commandDescriptorFixture 提供可控命令声明并实现对应业务接口
type commandDescriptorFixture struct {
	descriptor Descriptor // 当前测试使用的固定声明
}

// Descriptor 返回当前测试的命令声明
func (p commandDescriptorFixture) Descriptor() Descriptor { return p.descriptor }

// OnCommand 满足命令声明对应的业务接口
func (commandDescriptorFixture) OnCommand(context.Context, Command) (CommandResult, error) {
	return CommandResult{}, nil
}

// descriptorOnlyFixture 只提供自动能力检测所需的固定声明
type descriptorOnlyFixture struct {
	descriptor Descriptor // 当前测试使用的固定声明
}

// Descriptor 返回当前测试的固定声明
func (p descriptorOnlyFixture) Descriptor() Descriptor { return p.descriptor }

// defaultsFixture 仅实现默认配置接口
type defaultsFixture struct{ descriptorOnlyFixture }

// ConfigDefaults 返回显式空默认值
func (defaultsFixture) ConfigDefaults() []byte { return []byte{} }

// validateFixture 仅实现配置校验接口
type validateFixture struct{ descriptorOnlyFixture }

// ConfigValidate 接受任意候选值
func (validateFixture) ConfigValidate(context.Context, []byte) error { return nil }

// applyFixture 仅实现配置应用接口
type applyFixture struct{ descriptorOnlyFixture }

// ConfigApply 接受任意候选值
func (applyFixture) ConfigApply(context.Context, []byte) error { return nil }

// panicConfigFixture 在配置应用开始时模拟插件 panic
type panicConfigFixture struct{ descriptorOnlyFixture }

// ConfigApply 触发带私有信息的 panic
func (panicConfigFixture) ConfigApply(context.Context, []byte) error {
	panic("private configuration data")
}

// lifecycleFixture 观察取消后的排队控制操作是否仍进入用户代码
type lifecycleFixture struct {
	enabled    atomic.Int32          // 已实际进入的启用调用数
	invocation chan *wire.Invocation // 可选的生命周期凭据观察通道
}

// Descriptor 提供测试固定声明
func (*lifecycleFixture) Descriptor() Descriptor { return Descriptor{ID: "fixture"} }

// TestCommandDescriptorValidationAtControlBoundary 验证 Describe 与 Initialize 共用已缓存的命令校验结果
func TestCommandDescriptorValidationAtControlBoundary(t *testing.T) {
	tests := []struct {
		name     string
		commands []CommandSpec
		category wire.Category
	}{
		{
			name: "invalid command",
			commands: []CommandSpec{{Path: []string{"fixture"}, Args: []ArgSpec{{
				Name: "count", Type: ArgTypeInt, Default: "invalid",
			}}}},
			category: wire.Category_CATEGORY_INVALID_ARGUMENT,
		},
		{
			name: "duplicate path",
			commands: []CommandSpec{
				{Path: []string{"fixture", "echo"}},
				{Path: []string{"fixture", "echo"}},
			},
			category: wire.Category_CATEGORY_ALREADY_EXISTS,
		},
		{
			name:     "reserved namespace",
			commands: []CommandSpec{{Path: []string{"golem", "status"}}},
			category: wire.Category_CATEGORY_INVALID_ARGUMENT,
		},
		{
			name: "unknown argument type",
			commands: []CommandSpec{{Path: []string{"fixture"}, Args: []ArgSpec{{
				Name: "value", Type: ArgType(99),
			}}}},
			category: wire.Category_CATEGORY_INVALID_ARGUMENT,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &controlServer{implementation: commandDescriptorFixture{descriptor: Descriptor{ID: "fixture", Commands: test.commands}}}
			described, _ := server.Describe(t.Context(), nil)
			if described.GetError().GetCategory() != test.category || described.GetDescriptor_() != nil {
				t.Fatalf("Describe accepted invalid commands: %v", described)
			}
			initialized, _ := server.Initialize(t.Context(), &wire.InitializeRequest{
				BrokerId: 1, ControlToken: "control", InstanceGeneration: 1, DataDir: t.TempDir(),
			})
			if initialized.GetError().GetCategory() != test.category {
				t.Fatalf("Initialize bypassed cached command validation: %v", initialized)
			}
		})
	}
}

// TestDescribeCachesIndependentDescriptorSnapshot 验证声明源和返回值均不能改写已校验缓存
func TestDescribeCachesIndependentDescriptorSnapshot(t *testing.T) {
	implementation := &commandDescriptorFixture{descriptor: Descriptor{
		ID: "fixture", Commands: []CommandSpec{{Path: []string{"fixture", "echo"}}},
	}}
	server := &controlServer{implementation: implementation}
	first, _ := server.Describe(t.Context(), nil)
	if first.GetError() != nil || first.GetDescriptor_() == nil {
		t.Fatalf("valid descriptor was rejected: %v", first)
	}

	implementation.descriptor.Commands[0].Path[0] = "golem"
	first.Descriptor_.Commands[0].Path[0] = "changed"
	second, _ := server.Describe(t.Context(), nil)
	if got := second.GetDescriptor_().GetCommands()[0].GetPath(); len(got) != 2 || got[0] != "fixture" || got[1] != "echo" {
		t.Fatalf("cached descriptor was mutated through an external declaration: %v", got)
	}
}

// TestDescribeReportsImplementedConfigInterfaces 验证能力标志来自实际接口而不是自报字段
func TestDescribeReportsImplementedConfigInterfaces(t *testing.T) {
	tests := []struct {
		name                      string
		implementation            Plugin
		defaults, validate, apply bool
	}{
		{name: "none", implementation: descriptorOnlyFixture{descriptor: Descriptor{ID: "fixture"}}},
		{name: "defaults", implementation: defaultsFixture{descriptorOnlyFixture{descriptor: Descriptor{ID: "fixture"}}}, defaults: true},
		{name: "validate", implementation: validateFixture{descriptorOnlyFixture{descriptor: Descriptor{ID: "fixture"}}}, validate: true},
		{name: "apply", implementation: applyFixture{descriptorOnlyFixture{descriptor: Descriptor{ID: "fixture"}}}, apply: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := &controlServer{implementation: test.implementation}
			response, _ := server.Describe(t.Context(), nil)
			descriptor := response.GetDescriptor_()
			if response.GetError() != nil || descriptor == nil {
				t.Fatalf("Describe failed: %v", response.GetError())
			}
			if descriptor.GetHasConfigDefaults() != test.defaults ||
				descriptor.GetHasConfigValidate() != test.validate ||
				descriptor.GetHasConfigApply() != test.apply {
				t.Fatalf("capability mismatch: %+v", descriptor)
			}
		})
	}
}

// TestConfigApplyPanicIsOutcomeUnknown 验证回调 panic 保留未知结果且不泄漏私有信息
func TestConfigApplyPanicIsOutcomeUnknown(t *testing.T) {
	s, completion := newExecutionFixture(t, panicConfigFixture{descriptorOnlyFixture{descriptor: Descriptor{ID: "fixture"}}})
	response, _ := s.ConfigApply(t.Context(), &wire.ConfigCandidateRequest{
		InvocationId: "panic-apply", Candidate: &wire.BytesValue{Value: []byte("value=true")},
	})
	value := response.GetError()
	if value.GetCategory() != wire.Category_CATEGORY_INTERNAL ||
		value.GetOutcome() != wire.WriteOutcome_WRITE_OUTCOME_UNKNOWN ||
		value.GetOperation() != "plugin.config_apply" ||
		strings.Contains(value.GetReason(), "private") {
		t.Fatalf("panic 结果=%+v", value)
	}
	<-completion.completed

	invalid, _ := s.ConfigApply(t.Context(), &wire.ConfigCandidateRequest{})
	if invalid.GetError().GetCategory() != wire.Category_CATEGORY_INVALID_ARGUMENT ||
		invalid.GetError().GetOutcome() != wire.WriteOutcome_WRITE_OUTCOME_NOT_APPLICABLE {
		t.Fatalf("回调前错误被标成未知结果: %+v", invalid.GetError())
	}
}

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
