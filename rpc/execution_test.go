package rpc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/plugin"
)

// executionPlugin 通过真实进程验证上下文和实际执行，控制方法显式释放忽略取消的业务
type executionPlugin struct {
	fixturePlugin // 复用人工配置与固定声明

	task           context.Context // 保存上次启用周期以核对重启用
	release        chan struct{}   // 真实退出许可，业务故意忽略原请求取消
	releaseOnce    sync.Once       // 保证控制释放可重复调用
	invocationMu   sync.Mutex      // 保护并行能力测试保存的旧上下文
	lastInvocation context.Context // 模拟插件保存并主动剥离取消的旧用户上下文
}

// Descriptor 显式声明三个已有实现的业务接口
func (p *executionPlugin) Descriptor() plugin.Descriptor {
	d := p.fixturePlugin.Descriptor()
	d.Subscriptions = []event.Subscription{{Topic: "fixture", Mode: event.SubscribeModeRespond}}
	d.Commands = []command.CommandSpec{{Path: []string{"fixture"}}}
	d.Capabilities = []capability.CapabilitySpec{{Name: "fixture"}}
	return d
}

// OnLoad 读取自动绑定的 Host 并确保启用前没有可用的后台周期
func (p *executionPlugin) OnLoad(ctx context.Context) error {
	host := p.Host
	if host == nil {
		return errors.New("Host was not injected before OnLoad")
	}
	if host.TaskContext() == nil || host.TaskContext().Err() == nil {
		return errors.New("background context active before enable")
	}
	return p.fixturePlugin.OnLoad(ctx)
}

// OnEnable 检查后台周期不继承已返回的生命周期 RPC
func (p *executionPlugin) OnEnable(ctx context.Context) error {
	current := p.Host.TaskContext()
	if current == nil || current.Err() != nil || current == p.task || (p.task != nil && p.task.Err() == nil) {
		return errors.New("enable did not replace canceled task context")
	}
	p.task = current
	return nil
}

// OnDisable 在用户禁用回调之前后台周期必须已经取消
func (p *executionPlugin) OnDisable(ctx context.Context) error {
	if p.task.Err() == nil || ctx.Err() != nil {
		return errors.New("disable did not separate task and RPC contexts")
	}
	return nil
}

// OnEvent 在一个 token 下调用七类 Host 客户端的全部一元方法
func (p *executionPlugin) OnEvent(ctx context.Context, value event.Event) (event.EventResult, error) {
	current := p.Host.TaskContext()
	if current == nil || current.Err() != nil {
		return event.EventResult{}, errors.New("lifecycle RPC canceled task context")
	}
	p.task = current
	if _, err := p.Host.Account.Current(ctx); err != nil {
		return event.EventResult{}, err
	}
	return event.EventResult{Consumed: true}, exerciseHost(ctx, p.Host)
}

// OnCommand 通过反向能力调用传播父请求取消，错误保留类别和机器元数据
func (p *executionPlugin) OnCommand(ctx context.Context, value command.Command) (command.CommandResult, error) {
	_, err := p.Host.Caller.Call(ctx, capability.CallRequest{CapabilityName: "nested"})
	return command.CommandResult{}, err
}

// OnCall 指定场景故意忽略取消，直到独立配置控制释放
func (p *executionPlugin) OnCall(ctx context.Context, request capability.CallRequest) (capability.CallResult, error) {
	p.invocationMu.Lock()
	p.lastInvocation = ctx
	p.invocationMu.Unlock()
	if request.CapabilityName == "ignore" {
		if _, err := p.Host.Account.Current(ctx); err != nil {
			return capability.CallResult{}, err
		}
		<-p.release
		return capability.CallResult{Payload: []byte("exited")}, nil
	}
	return capability.CallResult{}, &sdk.Error{Category: sdk.CategoryConflict, Operation: "fixture.call", InvocationID: "diagnostic", Outcome: sdk.OutcomeFailed, RetryAfter: time.Second, Reason: "fixture conflict"}
}

// ConfigApply 控制面即使业务额度满也能执行，并允许忽略取消的代码退出
func (p *executionPlugin) ConfigApply(ctx context.Context, data []byte) error {
	if string(data) == "stale" {
		p.invocationMu.Lock()
		saved := p.lastInvocation
		p.invocationMu.Unlock()
		_, err := p.Host.Account.Current(context.WithoutCancel(saved))
		if !hasCategory(err, sdk.CategoryPermissionDenied) {
			return errors.New("stale invocation retained user authority")
		}
		return nil
	}
	if string(data) == "release" {
		p.releaseOnce.Do(func() { close(p.release) })
		return nil
	}
	if string(data) == "fail" {
		return &sdk.Error{Category: sdk.CategoryInvalidArgument, Operation: "fixture.config_apply", Outcome: sdk.OutcomeFailed, Reason: "controlled apply failure"}
	}
	if string(data) == "unknown" {
		return errors.New("controlled unknown apply result")
	}
	return p.fixturePlugin.ConfigApply(ctx, data)
}

// fullFixtureHost 为真实反向连接提供七类人工业务实现
func fullFixtureHost() *plugin.Host {
	return &plugin.Host{Messages: fixtureMessages{}, Contacts: fixtureContacts{}, Chatrooms: fixtureChatrooms{}, Sessions: fixtureSessions{}, Caller: fixtureCaller{}, Config: fixtureConfig{}, Account: &fixtureAccount{}}
}

// TestInvocationAcrossAllHostClients 验证当前 token 自动透传、父期限和后台启用周期
func TestInvocationAcrossAllHostClients(t *testing.T) {
	options := fixtureOptions(t, "execution")
	options.Services.Host = fullFixtureHost()
	var mu sync.Mutex
	methods := map[string]bool{}
	lifecycle := map[string]string{}
	deadline := time.Now().Add(time.Minute)
	options.Services.RegisterLifecycle = func(_ context.Context, invocation Invocation, _ LifecycleOperation) (func(), error) {
		mu.Lock()
		lifecycle[invocation.ID] = invocation.Token
		mu.Unlock()
		return func() {
			mu.Lock()
			delete(lifecycle, invocation.ID)
			mu.Unlock()
		}, nil
	}
	options.Services.Authorize = func(ctx context.Context, invocation Invocation, method string) (context.Context, error) {
		mu.Lock()
		lifecycleToken, lifecycleCall := lifecycle[invocation.ID]
		mu.Unlock()
		if lifecycleCall && invocation.Token == lifecycleToken && method == wire.HostAccountService_Current_FullMethodName {
			return ctx, nil // 仅允许测试的 OnLoad 凭据读取人工账号
		}
		if !strings.HasPrefix(invocation.ID, "event-") || invocation.Token != "host-issued" {
			return nil, sdk.NewError(sdk.CategoryPermissionDenied, "fixture.authorize", "invalid invocation")
		}
		actual, ok := ctx.Deadline()
		if !ok || actual.After(deadline) {
			return nil, errors.New("nested host call extended parent deadline")
		}
		mu.Lock()
		methods[method] = true
		mu.Unlock()
		return ctx, nil
	}
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for i := range 2 {
		if err := client.Enable(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		result, err := client.HandleEvent(ctx, Invocation{ID: fmt.Sprint("event-", i), Token: "host-issued"}, event.Event{Topic: "fixture", OccurredAt: fixtureTime()})
		cancel()
		if err != nil || !result.Consumed {
			t.Fatalf("event failed: %v", err)
		}
		if err := client.Disable(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 18 {
		t.Fatalf("only %d/18 unary host methods carried invocation", len(methods))
	}
}

// nestedCaller 观察嵌套能力调用继承的取消
type nestedCaller struct {
	entered  chan struct{} // 子调用开始信号
	canceled chan struct{} // 子调用实际收到取消的信号
}

// Call 等待原始请求取消，避免使用任意 sleep 推测顺序
func (n nestedCaller) Call(ctx context.Context, _ capability.CallRequest) (*capability.CallResult, error) {
	close(n.entered)
	<-ctx.Done()
	close(n.canceled)
	return nil, ctx.Err()
}

// TestNestedBusinessCancellation 验证宿主到插件再反向调用的完整取消链和错误类别
func TestNestedBusinessCancellation(t *testing.T) {
	options := fixtureOptions(t, "execution")
	nested := nestedCaller{entered: make(chan struct{}), canceled: make(chan struct{})}
	options.Services.Host.Caller = nested
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Enable(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := client.HandleCommand(ctx, Invocation{ID: "nested", Token: "token"}, command.Command{})
		finished <- err
	}()
	<-nested.entered
	cancel()
	if err := <-finished; !hasCategory(err, sdk.CategoryCanceled) {
		t.Fatalf("cancellation category lost: %v", err)
	}
	<-nested.canceled
	_, err = client.HandleCapability(t.Context(), Invocation{ID: "error", Token: "token"}, capability.CallRequest{CapabilityName: "error"})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryConflict || business.Operation != "fixture.call" || business.InvocationID != "diagnostic" || business.Outcome != sdk.OutcomeFailed || business.RetryAfter != time.Second {
		t.Fatalf("business error metadata lost: %v", err)
	}
	if err := client.ConfigApply(t.Context(), []byte("stale")); err != nil {
		t.Fatal(err)
	}
}

// TestConfigProviderResumeSemantics 验证确定失败恢复准入、未知结果保持关闭，并绑定新启用周期。
func TestConfigProviderResumeSemantics(t *testing.T) {
	options := fixtureOptions(t, "execution")
	options.Services.Host = fullFixtureHost()
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.EnableWithBackgroundToken(t.Context(), "cycle-one"); err != nil {
		t.Fatal(err)
	}
	// 配置屏障由宿主在调用 ApplyAndResume 前建立；这里显式模拟该步骤。
	if _, err := client.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	provider := NewConfigProvider(client)
	if err := provider.ApplyAndResume(t.Context(), []byte("release"), "cycle-two"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.HandleEvent(t.Context(), Invocation{ID: "after-success", Token: "token"}, event.Event{Topic: "fixture", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatalf("successful apply did not restore admission: %v", err)
	}

	if _, err := client.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	err = provider.ApplyAndResume(t.Context(), []byte("fail"), "cycle-three")
	if !hasCategory(err, sdk.CategoryInvalidArgument) {
		t.Fatalf("determinate apply failure category lost: %v", err)
	}
	if _, err := client.HandleEvent(t.Context(), Invocation{ID: "after-determinate", Token: "token"}, event.Event{Topic: "fixture", OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatalf("determinate failure did not restore old admission: %v", err)
	}

	if _, err := client.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	err = provider.ApplyAndResume(t.Context(), []byte("unknown"), "cycle-four")
	var value *sdk.Error
	if !errors.As(err, &value) || value.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("unknown apply result not marked unknown: %v", err)
	}
	if _, err := client.HandleEvent(t.Context(), Invocation{ID: "after-unknown", Token: "token"}, event.Event{Topic: "fixture", OccurredAt: time.Now().UTC()}); err == nil {
		t.Fatal("unknown apply result reopened business admission")
	}
}

// TestConfigApplyErrorClassification 验证只有明确失败和未实现可视为确定结果。
func TestConfigApplyErrorClassification(t *testing.T) {
	determinate := &sdk.Error{Category: sdk.CategoryInvalidArgument, Operation: "fixture.apply", Outcome: sdk.OutcomeFailed}
	if !IsDeterminateConfigFailure(determinate) {
		t.Fatal("explicit failure was not classified as determinate")
	}
	unimplemented := sdk.NewError(sdk.CategoryUnimplemented, "fixture.apply", "not implemented")
	if !IsDeterminateConfigFailure(unimplemented) {
		t.Fatal("unimplemented apply was not classified as determinate")
	}
	for _, err := range []error{
		nil,
		errors.New("transport failure"),
		&sdk.Error{Category: sdk.CategoryConflict, Operation: "fixture.apply", Outcome: sdk.OutcomeNotApplicable},
		context.DeadlineExceeded,
	} {
		if IsDeterminateConfigFailure(err) {
			t.Fatalf("error was incorrectly treated as determinate: %v", err)
		}
	}
}

// executionAccount 提供插件已经开始执行的可观察回调
type executionAccount struct {
	entered chan struct{} // 已进入业务执行的信号
}

// Current 通知测试插件已开始并返回人工账号
func (a executionAccount) Current(ctx context.Context) (*sdk.Account, error) {
	if a.entered != nil {
		a.entered <- struct{}{}
	}
	return (&fixtureAccount{}).Current(ctx)
}

// TestTimeoutRetainsExecutionUntilLateCompletion 验证 RPC 超时后仍占额度，迟到确认独立于业务取消，Drain 可核对归零
func TestTimeoutRetainsExecutionUntilLateCompletion(t *testing.T) {
	options := fixtureOptions(t, "execution")
	entered := make(chan struct{}, 4)
	options.Services.Host.Account = executionAccount{entered: entered}
	completed := make(chan string, 4)
	options.Services.ExecutionCompleted = func(ctx context.Context, id string, generation uint64) error {
		if id == "ignore-0" || id == "ignore-1" {
			if ctx.Err() != nil || generation != 1 {
				return errors.New("completion inherited canceled business context")
			}
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("completion has no deadline")
			}
			completed <- id
		}
		return nil
	}
	client, err := Start(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	<-entered // OnLoad 账号查询已返回
	if err := client.Enable(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		// 等 handler 进入后再使父 deadline 到期，避免调度速度决定是否进入用户代码
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(time.Second))
		finished := make(chan error, 1)
		go func() {
			_, err := client.HandleCapability(ctx, Invocation{ID: fmt.Sprint("ignore-", i), Token: "token"}, capability.CallRequest{CapabilityName: "ignore"})
			finished <- err
		}()
		<-entered
		err := <-finished
		cancel()
		if !hasCategory(err, sdk.CategoryDeadlineExceeded) {
			t.Fatalf("expected timeout: %v", err)
		}
	}
	_, err = client.HandleCapability(t.Context(), Invocation{ID: "full", Token: "token"}, capability.CallRequest{CapabilityName: "error"})
	if !hasCategory(err, sdk.CategoryResourceExhausted) {
		t.Fatalf("timeout prematurely released actual quota: %v", err)
	}
	if err := client.ConfigApply(t.Context(), []byte("stale")); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-completed:
		t.Fatalf("false early completion: %s", id)
	default:
	}
	active, err := client.Drain(t.Context())
	if err != nil || !slices.Equal(active, []string{"ignore-0", "ignore-1"}) {
		t.Fatalf("drain lost running code: %v %v", active, err)
	}
	if err := client.ConfigApply(t.Context(), []byte("release")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		<-completed
	}
	active, err = client.Drain(t.Context())
	if err != nil || len(active) != 0 {
		t.Fatalf("actual handlers did not drain: %v %v", active, err)
	}
}

// TestOldGenerationCompletionDenied 验证旧代次和错误控制凭据不能释放新代次同名执行
func TestOldGenerationCompletionDenied(t *testing.T) {
	called := false
	control := &hostControl{token: "new-control", generation: 9, completed: func(context.Context, string, uint64) error { called = true; return nil }}
	for _, request := range []*wire.ExecutionCompletedRequest{
		{ControlToken: "new-control", InstanceGeneration: 8, InvocationId: "same-id"},
		{ControlToken: "old-control", InstanceGeneration: 9, InvocationId: "same-id"},
	} {
		response, _ := control.ExecutionCompleted(t.Context(), request)
		if response.GetError().GetCategory() != wire.Category_CATEGORY_PERMISSION_DENIED || called {
			t.Fatal("old completion reached new generation execution registry")
		}
	}
	response, _ := control.ExecutionCompleted(t.Context(), &wire.ExecutionCompletedRequest{ControlToken: "new-control", InstanceGeneration: 9, InvocationId: "same-id"})
	if response.GetError() != nil || !called {
		t.Fatal("valid generation completion was rejected")
	}
}

// TestHandleCapabilityOversizedCategory 验证合法身份的超限载荷在进入传输前返回资源耗尽
func TestHandleCapabilityOversizedCategory(t *testing.T) {
	client := &Client{}
	_, err := client.HandleCapability(t.Context(), Invocation{ID: "oversized", Token: "token"}, capability.CallRequest{
		Payload: make([]byte, MaxInlinePayloadSize+1),
	})
	if !hasCategory(err, sdk.CategoryResourceExhausted) {
		t.Fatalf("超限载荷错误类别不正确: %v", err)
	}
}
