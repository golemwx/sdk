package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
)

type runtimeFixture struct {
	*Base

	mu            sync.Mutex
	subscriptions []event.Subscription
	capabilities  []capability.Spec
	started       context.Context
	loadErr       error
	startErr      error
	stopErr       error
	unloadErr     error
	event         func(context.Context, event.Event) (event.Result, error)
	capability    func(context.Context, capability.Request) (capability.Result, error)
	loads         atomic.Int32
	starts        atomic.Int32
	stops         atomic.Int32
	unloads       atomic.Int32
}

type expiresAfterFirstCheckContext struct {
	context.Context

	checks atomic.Int32
}

func (c *expiresAfterFirstCheckContext) Err() error {
	if c.checks.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}

func (*runtimeFixture) Metadata() Metadata {
	return Metadata{ID: "runtime-fixture", Name: "运行时测试"}
}

func (p *runtimeFixture) Subscriptions() []event.Subscription {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]event.Subscription(nil), p.subscriptions...)
}

func (p *runtimeFixture) Capabilities() []capability.Spec {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]capability.Spec(nil), p.capabilities...)
}

func (p *runtimeFixture) OnLoad(context.Context) error {
	p.loads.Add(1)
	return p.loadErr
}

func (p *runtimeFixture) OnStart(ctx context.Context) error {
	p.starts.Add(1)
	p.mu.Lock()
	p.started = ctx
	p.mu.Unlock()
	return p.startErr
}

func (p *runtimeFixture) OnStop(context.Context) error {
	p.stops.Add(1)
	return p.stopErr
}

func (p *runtimeFixture) OnUnload(context.Context) error {
	p.unloads.Add(1)
	return p.unloadErr
}

func (p *runtimeFixture) OnEvent(ctx context.Context, value event.Event) (event.Result, error) {
	p.mu.Lock()
	handler := p.event
	p.mu.Unlock()
	if handler == nil {
		return event.Result{}, nil
	}
	return handler(ctx, value)
}

func (p *runtimeFixture) OnCapability(ctx context.Context, request capability.Request) (capability.Result, error) {
	p.mu.Lock()
	handler := p.capability
	p.mu.Unlock()
	if handler == nil {
		return capability.Result{}, nil
	}
	return handler(ctx, request)
}

func TestRuntimeLifecyclePublishesHostAndCycle(t *testing.T) {
	implementation := &runtimeFixture{Base: NewBase()}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	host := NewHost(nil, nil, nil, nil, nil, nil, nil)
	dataRoot := filepath.Join(t.TempDir(), "not-created")

	if err := instance.Initialize(t.Context(), InitializeOptions{Host: host, DataDir: dataRoot}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if implementation.Host != host {
		t.Fatal("Initialize() did not publish the instance host")
	}
	if implementation.Context() != nil {
		t.Fatal("Base.Context() must be nil before Start")
	}
	if _, err := os.Stat(dataRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Initialize() touched the plugin data directory: %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	firstCycle := implementation.Context()
	if firstCycle == nil || firstCycle.Err() != nil {
		t.Fatalf("Base.Context() = %v, want an active cycle", firstCycle)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if firstCycle.Err() == nil {
		t.Fatal("Stop() did not cancel the enable cycle")
	}

	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	secondCycle := implementation.Context()
	if secondCycle == firstCycle || secondCycle.Err() != nil {
		t.Fatal("second Start() did not publish a fresh enable cycle")
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
	if implementation.Host != host {
		t.Fatal("Unload() must retain the stable Host field")
	}
	if implementation.loads.Load() != 1 || implementation.starts.Load() != 2 ||
		implementation.stops.Load() != 2 || implementation.unloads.Load() != 1 {
		t.Fatalf("unexpected lifecycle counts: loads=%d starts=%d stops=%d unloads=%d",
			implementation.loads.Load(), implementation.starts.Load(),
			implementation.stops.Load(), implementation.unloads.Load())
	}
	if _, err := instance.HandleEvent(t.Context(), event.Event{Topic: event.Message, OccurredAt: time.Now()}); err == nil {
		t.Fatal("unloaded instance accepted a business event")
	}
	if err := instance.ValidateConfig(t.Context(), nil); err == nil {
		t.Fatal("ValidateConfig() unexpectedly succeeded before R05")
	}
}

func TestStopWaitsForHandlerThatIgnoresCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	implementation := &runtimeFixture{Base: NewBase()}
	implementation.event = func(context.Context, event.Event) (event.Result, error) {
		once.Do(func() { close(started) })
		<-release
		return event.Result{Consumed: true}, nil
	}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{
		Host: NewHost(nil, nil, nil, nil, nil, nil, nil), DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	eventDone := make(chan error, 1)
	go func() {
		_, eventErr := instance.HandleEvent(t.Context(), event.Event{Topic: event.Message, OccurredAt: time.Now()})
		eventDone <- eventErr
	}()
	<-started

	stopDone := make(chan error, 1)
	go func() { stopDone <- instance.Stop(t.Context()) }()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop() returned before the handler exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-eventDone; err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestUnloadDrainsWhenContextExpiresAfterInitialCheck(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	implementation := &runtimeFixture{Base: NewBase()}
	implementation.event = func(context.Context, event.Event) (event.Result, error) {
		close(started)
		<-release
		return event.Result{Consumed: true}, nil
	}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{
		Host: NewHost(nil, nil, nil, nil, nil, nil, nil), DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	eventDone := make(chan error, 1)
	go func() {
		_, eventErr := instance.HandleEvent(t.Context(), event.Event{Topic: event.Message, OccurredAt: time.Now()})
		eventDone <- eventErr
	}()
	<-started

	unloadDone := make(chan error, 1)
	go func() {
		unloadDone <- instance.Unload(&expiresAfterFirstCheckContext{Context: context.Background()})
	}()
	select {
	case err := <-unloadDone:
		t.Fatalf("Unload() returned before the handler exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-unloadDone; err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
	if err := <-eventDone; err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
}

func TestRemoteHostLossCancelsCycleAndRejectsCalls(t *testing.T) {
	state := newRemoteHostState(nil)
	host := newRemoteHost(state)
	implementation := &runtimeFixture{Base: NewBase()}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{Host: host, DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cycle := implementation.Context()
	reason := sdk.NewError(sdk.CategoryUnavailable, "plugin.host", "host disconnected")
	state.invalidate(reason)
	instance.hostLost(reason)
	if cycle.Err() == nil {
		t.Fatal("host loss did not cancel the enable cycle")
	}
	if err := state.check(t.Context(), "account.current"); err == nil {
		t.Fatal("invalidated remote host still accepts calls")
	}
	if _, err := instance.HandleEvent(t.Context(), event.Event{Topic: event.Message, OccurredAt: time.Now()}); err == nil {
		t.Fatal("invalidated instance accepted a business event")
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() after disconnect error = %v", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestStopDisablesRemoteHostCalls(t *testing.T) {
	state := newRemoteHostState(nil)
	host := newRemoteHost(state)
	instance, err := newRuntime(&runtimeFixture{Base: NewBase()})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{Host: host, DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	err = state.check(t.Context(), "account.current")
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryFailedPrecondition {
		t.Fatalf("check() after Stop = %v, want FailedPrecondition", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestDescriptorAutoDetectsImplementedInterfaces(t *testing.T) {
	implementation := &runtimeFixture{
		Base: NewBase(),
		subscriptions: []event.Subscription{
			{Topic: event.Message, Mode: event.Consume, Priority: -1},
		},
		capabilities: []capability.Spec{{Name: "fixture.echo"}},
	}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	descriptor, err := instance.Describe(t.Context())
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if len(descriptor.Subscriptions) != 1 || len(descriptor.Capabilities) != 1 {
		t.Fatalf("Describe() = %+v", descriptor)
	}
	descriptor.Subscriptions[0].Priority = 99
	descriptor.Capabilities[0].Name = "changed"
	again, err := instance.Describe(t.Context())
	if err != nil {
		t.Fatalf("second Describe() error = %v", err)
	}
	if again.Subscriptions[0].Priority != -1 || again.Capabilities[0].Name != "fixture.echo" {
		t.Fatal("Describe() exposed mutable cached state")
	}
}

func TestDescriptorRejectsConflictingSubscriptionModes(t *testing.T) {
	implementation := &runtimeFixture{
		Base: NewBase(),
		subscriptions: []event.Subscription{
			{Topic: event.Message, Mode: event.Observe},
			{Topic: event.Message, Mode: event.Consume},
		},
	}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if _, err := instance.Describe(t.Context()); err == nil {
		t.Fatal("Describe() accepted conflicting subscription modes")
	}
}

func TestRuntimeProtectsAgainstHandlerPanic(t *testing.T) {
	implementation := &runtimeFixture{Base: NewBase()}
	implementation.event = func(context.Context, event.Event) (event.Result, error) {
		panic("private handler state")
	}
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{
		Host: NewHost(nil, nil, nil, nil, nil, nil, nil), DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = instance.HandleEvent(t.Context(), event.Event{Topic: event.Message, OccurredAt: time.Now()})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInternal {
		t.Fatalf("panic was not converted to Internal: %v", err)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestInstanceLockRejectsConcurrentOwner(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "run", "plugin-lock-test.lock")
	t.Setenv(LockPathEnv, lockPath)
	first, err := acquireInstanceLock("lock-test")
	if err != nil {
		t.Fatalf("first acquireInstanceLock() error = %v", err)
	}
	if _, err := acquireInstanceLock("lock-test"); err == nil {
		t.Fatal("second acquireInstanceLock() unexpectedly succeeded")
	}
	closeInstanceLock(first)
	second, err := acquireInstanceLock("lock-test")
	if err != nil {
		t.Fatalf("acquireInstanceLock() after release error = %v", err)
	}
	closeInstanceLock(second)
}

func TestInstanceLockPathUsesRunDirectory(t *testing.T) {
	root := t.TempDir()
	path, err := InstanceLockPath(root, "fixture")
	if err != nil {
		t.Fatalf("InstanceLockPath() error = %v", err)
	}
	want := filepath.Join(root, "run", "plugin-fixture.lock")
	if path != want {
		t.Fatalf("InstanceLockPath() = %q, want %q", path, want)
	}
}

func TestStartRejectsMissingHost(t *testing.T) {
	_, err := Start(t.Context(), ClientOptions{
		Command:  exec.Command("plugin-fixture"),
		PluginID: "fixture",
		DataRoot: t.TempDir(),
	})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
		t.Fatalf("Start() error = %v, want InvalidArgument", err)
	}
}

func TestUnimplementedOperationsDoNotReturnSuccess(t *testing.T) {
	instance, err := newRuntime(&runtimeFixture{Base: NewBase()})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{
		Host: NewHost(nil, nil, nil, nil, nil, nil, nil), DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	data := unsupportedData{}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "validate config", call: func() error { return instance.ValidateConfig(t.Context(), nil) }},
		{name: "set config", call: func() error { return instance.SetConfig(t.Context(), nil) }},
		{name: "handle command", call: func() error {
			return instance.HandleCommand(t.Context(), command.Invocation{})
		}},
		{name: "data open", call: func() error { _, err := data.Open("file", os.O_RDONLY, 0); return err }},
		{name: "data read", call: func() error { _, err := data.ReadFile("file"); return err }},
		{name: "data write", call: func() error { return data.WriteFile("file", nil, 0o600) }},
		{name: "data remove", call: func() error { return data.Remove("file") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var business *sdk.Error
			if err := test.call(); !errors.As(err, &business) || business.Category != sdk.CategoryUnimplemented {
				t.Fatalf("call error = %v, want Unimplemented", err)
			}
		})
	}
}
