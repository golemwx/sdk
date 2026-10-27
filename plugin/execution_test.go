package plugin

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// wireTime 返回合法的人工事件时间
func wireTime() *timestamppb.Timestamp { return timestamppb.New(time.Unix(1, 0)) }

// blockingPlugin 用显式信号控制忽略取消的真实业务执行
type blockingPlugin struct {
	entered     chan string   // 标记用户代码已开始
	release     chan struct{} // 允许用户代码真正退出
	panicOnCall bool          // 指定能力处理是否触发 panic
}

// Descriptor 提供三个业务接口的人工声明
func (*blockingPlugin) Descriptor() Descriptor { return Descriptor{ID: "blocking"} }

// OnEvent 故意忽略取消，直到测试明确允许退出
func (p *blockingPlugin) OnEvent(ctx context.Context, value Event) (EventResult, error) {
	p.entered <- transport.InvocationFromContext(ctx).Id
	<-p.release
	return EventResult{Consumed: true}, nil
}

// OnCommand 与事件共用同一阻塞业务逻辑
func (p *blockingPlugin) OnCommand(ctx context.Context, _ Command) (CommandResult, error) {
	_, err := p.OnEvent(ctx, Event{})
	return CommandResult{}, err
}

// OnCall 使用能力额度并可模拟用户代码 panic
func (p *blockingPlugin) OnCall(ctx context.Context, _ CallRequest) (CallResult, error) {
	if p.panicOnCall {
		panic("private handler data")
	}
	_, err := p.OnEvent(ctx, Event{})
	return CallResult{}, err
}

// completionFixture 观察完成通知是否脱离业务取消并带上正确实例身份
type completionFixture struct {
	wire.HostControlServiceClient // 只实现本测试需要的完成确认

	completed chan string // 收集真实退出通知
	t         *testing.T  // 当前测试实例
	fail      bool        // 模拟控制通知失败
}

// ExecutionCompleted 校验控制身份、期限及取消状态
func (f *completionFixture) ExecutionCompleted(ctx context.Context, request *wire.ExecutionCompletedRequest, _ ...grpc.CallOption) (*wire.OperationResponse, error) {
	deadline, ok := ctx.Deadline()
	if ctx.Err() != nil || !ok || time.Until(deadline) > completionTimeout || request.GetControlToken() != "control" || request.GetInstanceGeneration() != 7 || transport.InvocationFromContext(ctx) != nil {
		f.t.Error("completion lost independent bounded instance context")
	}
	f.completed <- request.InvocationId
	if f.fail {
		return nil, context.DeadlineExceeded
	}
	return &wire.OperationResponse{}, nil
}

// newExecutionFixture 创建没有真实网络依赖的 wrapper，仍实际执行用户 handler
func newExecutionFixture(t *testing.T, implementation Plugin) (*controlServer, *completionFixture) {
	t.Helper()
	completion := &completionFixture{t: t, completed: make(chan string, 16)}
	control, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	host := &Host{}
	host.startTasks(control)
	return &controlServer{implementation: implementation, conn: &grpc.ClientConn{}, host: host, loaded: true, enabled: true, accepting: true, controlToken: "control", generation: 7, controlContext: control, controlCancel: cancel, completion: completion}, completion
}

// TestIgnoredCancellationRetainsActualQuotas 验证忽略取消仍占额度，命令共享事件额度，能力额度独立
func TestIgnoredCancellationRetainsActualQuotas(t *testing.T) {
	p := &blockingPlugin{entered: make(chan string, 6), release: make(chan struct{})}
	s, completion := newExecutionFixture(t, p)
	b := &businessServer{state: s}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var running sync.WaitGroup
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(p.release) }); running.Wait() })
	for i := range 4 {
		running.Go(func() {
			response, _ := b.HandleEvent(ctx, &wire.EventRequest{InvocationId: fmt.Sprint("event-", i), InvocationToken: "token", Event: &wire.Event{OccurredAt: wireTime()}})
			if response.GetError() != nil {
				t.Errorf("event failed: %v", response.GetError())
			}
		})
	}
	for range 4 {
		<-p.entered
	}
	cancel()
	command, _ := b.HandleCommand(t.Context(), &wire.CommandRequest{InvocationId: "command", InvocationToken: "token", Command: &wire.Command{}})
	if command.GetError().GetCategory() != wire.Category_CATEGORY_RESOURCE_EXHAUSTED {
		t.Fatal("cancellation released ordinary business quota")
	}
	for i := range 2 {
		running.Go(func() {
			response, _ := b.HandleCapability(t.Context(), &wire.CapabilityRequest{InvocationId: fmt.Sprint("call-", i), InvocationToken: "token", Request: &wire.CallRequest{}})
			if response.GetError() != nil {
				t.Errorf("independent capability failed: %v", response.GetError())
			}
		})
	}
	for range 2 {
		<-p.entered
	}
	call, _ := b.HandleCapability(t.Context(), &wire.CapabilityRequest{InvocationId: "overflow", InvocationToken: "token", Request: &wire.CallRequest{}})
	if call.GetError().GetCategory() != wire.Category_CATEGORY_RESOURCE_EXHAUSTED {
		t.Fatal("capability quota was not enforced")
	}
	select {
	case <-completion.completed:
		t.Fatal("completion sent before handler exit")
	default:
	}
	drain, _ := s.Drain(t.Context(), &wire.DrainRequest{ControlToken: "control", InstanceGeneration: 7})
	if drain.GetError() != nil || len(drain.GetActiveInvocationIds()) != 6 || s.host.TaskContext().Err() == nil {
		t.Fatal("drain lost actual executions or background cancellation")
	}
	response, _ := b.HandleEvent(t.Context(), &wire.EventRequest{InvocationId: "late", InvocationToken: "token", Event: &wire.Event{OccurredAt: wireTime()}})
	if response.GetError().GetCategory() != wire.Category_CATEGORY_FAILED_PRECONDITION {
		t.Fatal("drain admitted new user code")
	}
	release.Do(func() { close(p.release) })
	running.Wait()
	for range 6 {
		<-completion.completed
	}
	drain, _ = s.Drain(t.Context(), &wire.DrainRequest{ControlToken: "control", InstanceGeneration: 7})
	if len(drain.GetActiveInvocationIds()) != 0 {
		t.Fatal("actual executions leaked after exit")
	}
}

// TestPanicReleasesExecution 验证 panic 仍确认真实退出，重复调用标识不能覆盖在途执行
func TestPanicReleasesExecution(t *testing.T) {
	s, completion := newExecutionFixture(t, &blockingPlugin{panicOnCall: true})
	b := &businessServer{state: s}
	response, _ := b.HandleCapability(t.Context(), &wire.CapabilityRequest{InvocationId: "panic", InvocationToken: "token", Request: &wire.CallRequest{}})
	if response.GetError().GetCategory() != wire.Category_CATEGORY_INTERNAL || response.GetError().GetOperation() != "plugin.capability" || <-completion.completed != "panic" || len(s.activeExecutions()) != 0 {
		t.Fatal("panic leaked execution or private error")
	}
	if err := s.beginExecution(t.Context(), "duplicate", executionBusiness); err != nil {
		t.Fatal(err)
	}
	if err := s.beginExecution(t.Context(), "duplicate", executionCapability); err == nil || !slices.Equal(s.activeExecutions(), []string{"duplicate"}) {
		t.Fatal("duplicate ID overwrote active execution")
	}
}

// TestCompletionFailureCanReconcileWithDrain 验证通知失败不伪造未退出的业务，宿主可用 Drain 核对
func TestCompletionFailureCanReconcileWithDrain(t *testing.T) {
	s, completion := newExecutionFixture(t, &blockingPlugin{panicOnCall: true})
	completion.fail = true
	response, _ := (&businessServer{state: s}).HandleCapability(t.Context(), &wire.CapabilityRequest{InvocationId: "failed-notification", InvocationToken: "token", Request: &wire.CallRequest{}})
	if response.GetError().GetCategory() != wire.Category_CATEGORY_INTERNAL || <-completion.completed != "failed-notification" {
		t.Fatal("handler failure was lost")
	}
	drain, _ := s.Drain(t.Context(), &wire.DrainRequest{ControlToken: "control", InstanceGeneration: 7})
	if drain.GetError() != nil || len(drain.GetActiveInvocationIds()) != 0 {
		t.Fatal("failed completion notification leaked actual execution")
	}
}

// enablePlugin 观察启用前已创建的周期，以及排空与启用的竞争
type enablePlugin struct {
	host    *Host                // 显式宿主对象
	entered chan context.Context // 可选的启用就绪信号
	release chan struct{}        // 可选的启用退出许可
	fail    bool                 // 指定启用是否失败
}

// TestCanceledEnableImmediatelyCancelsTasks 验证启用 handler 忽略取消时后台周期立即结束，执行记录仍保留
func TestCanceledEnableImmediatelyCancelsTasks(t *testing.T) {
	p := &enablePlugin{entered: make(chan context.Context, 1), release: make(chan struct{})}
	s, _ := newExecutionFixture(t, p)
	p.host = s.host
	s.enabled, s.accepting = false, false
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan *wire.OperationResponse, 1)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(p.release) }) })
	go func() {
		response, _ := s.OnEnable(lifecycleIncomingContext(ctx, "canceled-enable", "token"), &wire.LifecycleRequest{InvocationId: "canceled-enable"})
		finished <- response
	}()
	task := <-p.entered
	cancel()
	guard, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	select {
	case <-task.Done():
	case <-guard.Done():
		t.Fatal("canceled enable kept background tasks alive")
	}
	if !slices.Equal(s.activeExecutions(), []string{"canceled-enable"}) {
		t.Fatal("enable cancellation pretended handler already exited")
	}
	release.Do(func() { close(p.release) })
	response := <-finished
	if response.GetError().GetCategory() != wire.Category_CATEGORY_CANCELED || s.accepting || s.enabled {
		t.Fatal("canceled enable changed error category or opened admission")
	}
}

// oversizedPlugin 模拟能力返回超限载荷及可选的原始业务错误
type oversizedPlugin struct {
	primary error // 需要优先保留的业务错误
}

// Descriptor 提供最小人工声明
func (*oversizedPlugin) Descriptor() Descriptor { return Descriptor{ID: "oversized"} }

// OnCall 返回超出内联限制的结果
func (p *oversizedPlugin) OnCall(context.Context, CallRequest) (CallResult, error) {
	return CallResult{Payload: make([]byte, transport.MaxInlinePayloadSize+1)}, p.primary
}

// TestOversizedResultPreservesPrimaryError 验证丢弃超限结果时仍保留原错误，成功超限才返回资源错误
func TestOversizedResultPreservesPrimaryError(t *testing.T) {
	for _, primary := range []error{nil, &sdk.Error{Category: sdk.CategoryConflict, Operation: "fixture.call", InvocationID: "original", Outcome: sdk.OutcomeUnknown}} {
		s, _ := newExecutionFixture(t, &oversizedPlugin{primary: primary})
		response, _ := (&businessServer{state: s}).HandleCapability(t.Context(), &wire.CapabilityRequest{InvocationId: "oversized", InvocationToken: "token", Request: &wire.CallRequest{}})
		if response.GetResult() != nil {
			t.Fatal("oversized payload escaped wrapper")
		}
		if primary == nil {
			if response.GetError().GetCategory() != wire.Category_CATEGORY_RESOURCE_EXHAUSTED {
				t.Fatal("successful oversized result did not fail")
			}
		} else if response.GetError().GetCategory() != wire.Category_CATEGORY_CONFLICT || response.GetError().GetInvocationId() != "original" || response.GetError().GetOutcome() != wire.WriteOutcome_WRITE_OUTCOME_UNKNOWN {
			t.Fatal("size validation replaced original business error")
		}
	}
}

// Descriptor 提供最小声明
func (*enablePlugin) Descriptor() Descriptor { return Descriptor{ID: "enable"} }

// OnEnable 仅使用启用周期运行后台任务，单次 RPC 上下文独立
func (p *enablePlugin) OnEnable(context.Context) error {
	if p.entered != nil {
		p.entered <- p.host.TaskContext()
		<-p.release
	}
	if p.fail {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "fixture.enable", "rejected")
	}
	return nil
}

// TestTaskContextCycles 验证单次 RPC 返回不取消后台，禁用取消，重启用创建新周期，启用失败取消
func TestTaskContextCycles(t *testing.T) {
	p := &enablePlugin{}
	s, _ := newExecutionFixture(t, p)
	p.host = s.host
	s.enabled = false
	s.accepting = false
	response, _ := s.OnEnable(lifecycleIncomingContext(t.Context(), "enable-1", "token"), &wire.LifecycleRequest{InvocationId: "enable-1"})
	first := s.host.TaskContext()
	if response.GetError() != nil || first == nil || first.Err() != nil || transport.InvocationFromContext(first) != nil {
		t.Fatal("enable did not create independent task context")
	}
	response, _ = s.OnDisable(lifecycleIncomingContext(t.Context(), "disable-1", "token"), &wire.LifecycleRequest{InvocationId: "disable-1"})
	if response.GetError() != nil || first.Err() == nil {
		t.Fatal("disable did not cancel task context")
	}
	response, _ = s.OnEnable(lifecycleIncomingContext(t.Context(), "enable-2", "token"), &wire.LifecycleRequest{InvocationId: "enable-2"})
	second := s.host.TaskContext()
	if response.GetError() != nil || second == first || second.Err() != nil {
		t.Fatal("re-enable reused canceled task context")
	}
	s.controlCancel()
	if second.Err() == nil {
		t.Fatal("instance invalidation kept task context alive")
	}
	failure := &enablePlugin{fail: true}
	s, _ = newExecutionFixture(t, failure)
	failure.host = s.host
	s.enabled = false
	s.accepting = false
	response, _ = s.OnEnable(lifecycleIncomingContext(t.Context(), "enable-fail", "token"), &wire.LifecycleRequest{InvocationId: "enable-fail"})
	if response.GetError() == nil || s.host.TaskContext().Err() == nil || s.enabled || s.accepting {
		t.Fatal("failed enable left active background context or admission")
	}
	// 旧轮次延迟到达的取消只能作用于原 context，不能撤销重新启用的新周期
	host := &Host{}
	cancelOld := host.startTasks(t.Context())
	old := host.TaskContext()
	cancelNew := host.startTasks(t.Context())
	defer cancelNew()
	cancelOld()
	if old.Err() == nil || host.TaskContext().Err() != nil {
		t.Fatal("late task cancellation affected a new enable cycle")
	}
}

// TestDrainWhileEnableIgnoresCancellation 验证 Drain 不等待生命周期锁，迟到启用不能重开准入
func TestDrainWhileEnableIgnoresCancellation(t *testing.T) {
	p := &enablePlugin{entered: make(chan context.Context, 1), release: make(chan struct{})}
	s, _ := newExecutionFixture(t, p)
	p.host = s.host
	s.enabled = false
	s.accepting = false
	finished := make(chan *wire.OperationResponse, 1)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(p.release) }) })
	go func() {
		response, _ := s.OnEnable(lifecycleIncomingContext(t.Context(), "enabling", "token"), &wire.LifecycleRequest{InvocationId: "enabling"})
		finished <- response
	}()
	task := <-p.entered
	drain, _ := s.Drain(t.Context(), &wire.DrainRequest{ControlToken: "control", InstanceGeneration: 7})
	if task.Err() == nil || !slices.Equal(drain.GetActiveInvocationIds(), []string{"enabling"}) {
		t.Fatal("drain did not cancel enabling task or lost lifecycle execution")
	}
	release.Do(func() { close(p.release) })
	response := <-finished
	if response.GetError() == nil || s.accepting || s.host.TaskContext().Err() == nil {
		t.Fatal("late enable reopened admission after drain")
	}
}

// missingHandlersPlugin 只有声明，没有业务接口
type missingHandlersPlugin struct {
	descriptor Descriptor // 当前子测试的声明
}

// Descriptor 返回缺少对应实现的声明
func (p missingHandlersPlugin) Descriptor() Descriptor { return p.descriptor }

// TestDeclaredHandlersRequired 验证三类声明必须有对应业务接口，生命周期仍可全部省略
func TestDeclaredHandlersRequired(t *testing.T) {
	for _, descriptor := range []Descriptor{
		{Subscriptions: []Subscription{{Topic: "message.received"}}},
		{Commands: []CommandSpec{{Path: []string{"test"}}}},
		{Capabilities: []CapabilitySpec{{Name: "test"}}},
	} {
		s := &controlServer{implementation: missingHandlersPlugin{descriptor: descriptor}}
		response, _ := s.Initialize(t.Context(), &wire.InitializeRequest{BrokerId: 1, ControlToken: "control", InstanceGeneration: 7, DataDir: t.TempDir()})
		if response.GetError().GetCategory() != wire.Category_CATEGORY_FAILED_PRECONDITION {
			t.Fatalf("missing handler was accepted: %v", response)
		}
	}
	s := &controlServer{implementation: missingHandlersPlugin{}}
	if err := s.validateHandlers(t.Context()); err != nil {
		t.Fatal(err)
	}
}
