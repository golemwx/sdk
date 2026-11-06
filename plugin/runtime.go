package plugin

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/wire"
)

// Runtime 的生产实现由 NewRuntime 创建。
//
// 插件进程应通过 Serve 启动；Serve 会在任何描述或业务调用前取得该插件
// 安装目录与 ID 对应的 OS 排他锁。NewRuntime 供同进程集成测试使用，不自行
// 持有进程锁。
type runtime struct {
	implementation Plugin
	metadata       Metadata

	descriptorOnce sync.Once
	descriptor     *wire.Descriptor
	descriptorErr  error

	lifecycleMu sync.Mutex
	mu          sync.Mutex

	initialized bool
	loaded      bool
	started     bool
	starting    bool
	stopping    bool
	accepting   bool
	unloaded    bool
	failed      *sdk.Error

	host        *Host
	cycle       context.Context
	cancelCycle context.CancelFunc
	handlerWait sync.WaitGroup
}

// NewRuntime 创建单个插件实例的运行时。
//
// 一个 Runtime 只能绑定一个插件实现。进程级单实例锁由 Serve 在启动
// go-plugin 协议前取得；直接使用 NewRuntime 不构成生产插件入口。
func NewRuntime(implementation Plugin) (Runtime, error) {
	return newRuntime(implementation)
}

func newRuntime(implementation Plugin) (*runtime, error) {
	if implementation == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.runtime", "plugin implementation is nil")
	}
	metadata, err := readMetadata(implementation)
	if err != nil {
		return nil, err
	}
	return &runtime{implementation: implementation, metadata: metadata}, nil
}

func readMetadata(implementation Plugin) (metadata Metadata, err error) {
	err = protectError("plugin.describe", func() error {
		metadata = implementation.Metadata()
		if !validPluginID(metadata.ID) {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "plugin ID is invalid")
		}
		return nil
	})
	if err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func validPluginID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for index := range len(id) {
		character := id[index]
		if character != '_' && character != '-' &&
			(character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// Describe 从实际实现的可选接口生成一次不可变描述快照。
func (r *runtime) Describe(ctx context.Context) (*Descriptor, error) {
	if err := contextError(ctx, "plugin.describe"); err != nil {
		return nil, err
	}
	r.descriptorOnce.Do(func() {
		r.descriptorErr = protectError("plugin.describe", func() error {
			value := Descriptor{Metadata: r.metadata}
			if handler, ok := r.implementation.(EventHandler); ok {
				subscriptions := cloneSubscriptions(handler.Subscriptions())
				if err := validateSubscriptions(subscriptions); err != nil {
					return err
				}
				value.Subscriptions = subscriptions
			}
			if provider, ok := r.implementation.(CapabilityProvider); ok {
				capabilities := cloneCapabilities(provider.Capabilities())
				if err := validateCapabilities(capabilities); err != nil {
					return err
				}
				value.Capabilities = capabilities
			}
			snapshot, err := toWireDescriptor(value)
			if err != nil {
				return err
			}
			r.descriptor = snapshot
			return nil
		})
	})
	if r.descriptorErr != nil {
		return nil, r.descriptorErr
	}
	if r.descriptor == nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.describe", "descriptor is unavailable")
	}
	value := fromWireDescriptor(r.descriptor)
	return &value, nil
}

// Initialize 绑定当前实例唯一的 Host。
//
// R03 只发布 Host；Config 和 DataDir 由 R05/R06 消费，当前不会读取配置或
// 创建数据目录。
func (r *runtime) Initialize(ctx context.Context, options InitializeOptions) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if err := contextError(ctx, "plugin.initialize"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.initialized || r.unloaded {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "plugin instance is already initialized")
	}
	if options.Host == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "host is nil")
	}
	if strings.TrimSpace(options.DataDir) == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "data root is empty")
	}
	if binder, ok := r.implementation.(interface{ bindHost(*Host) error }); ok {
		if err := binder.bindHost(options.Host); err != nil {
			return err
		}
	}
	r.host = options.Host
	r.initialized = true
	return nil
}

// Load 调用可选的 OnLoad，不创建数据目录或改变 Host 连接。
func (r *runtime) Load(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if err := contextError(ctx, "plugin.load"); err != nil {
		return err
	}
	r.mu.Lock()
	if !r.initialized || r.loaded || r.started || r.starting || r.unloaded {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.load", "plugin instance cannot be loaded")
	}
	if r.failed != nil {
		failure := cloneBusinessError(r.failed)
		r.mu.Unlock()
		return failure
	}
	handler, hasLoader := r.implementation.(Loader)
	r.mu.Unlock()

	if hasLoader {
		if err := protectError("plugin.load", func() error { return handler.OnLoad(ctx) }); err != nil {
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unloaded || r.failed != nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.load", "plugin instance became invalid while loading")
	}
	r.loaded = true
	return nil
}

// Start 建立启用周期并调用可选的 OnStart。
func (r *runtime) Start(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if err := contextError(ctx, "plugin.start"); err != nil {
		return err
	}
	r.mu.Lock()
	if !r.loaded || r.started || r.starting || r.unloaded {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.start", "plugin instance cannot be started")
	}
	if r.failed != nil {
		failure := cloneBusinessError(r.failed)
		r.mu.Unlock()
		return failure
	}
	cycle, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cycle, r.cancelCycle = cycle, cancel
	r.starting = true
	host := r.host
	r.mu.Unlock()

	publishCycle(r.implementation, cycle)
	if host != nil {
		host.setActive(true)
	}
	var startErr error
	if handler, ok := r.implementation.(Starter); ok {
		startErr = protectError("plugin.start", func() error { return handler.OnStart(cycle) })
	}
	if startErr != nil {
		cancel()
		if host != nil {
			host.setActive(false)
		}
		r.mu.Lock()
		r.starting = false
		r.mu.Unlock()
		return startErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	startErr = contextError(cycle, "plugin.start")
	if startErr == nil && (r.unloaded || r.failed != nil) {
		startErr = sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.start", "plugin instance became invalid while starting")
	}
	if startErr != nil {
		cancel()
		if host != nil {
			host.setActive(false)
		}
		r.starting = false
		return startErr
	}
	r.starting = false
	r.started = true
	r.accepting = true
	return nil
}

// Stop 取消启用周期、关闭业务准入并等待所有真实在途 handler 退出。
func (r *runtime) Stop(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if err := contextError(ctx, "plugin.stop"); err != nil {
		return err
	}
	return r.stopLocked(ctx)
}

func (r *runtime) stopLocked(ctx context.Context) error {
	r.mu.Lock()
	if r.unloaded {
		r.mu.Unlock()
		return nil
	}
	if !r.initialized || (!r.started && !r.starting) {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.stop", "plugin instance is not started")
	}
	r.stopping = true
	r.started = false
	r.starting = false
	r.accepting = false
	cancel := r.cancelCycle
	host := r.host
	r.mu.Unlock()

	if host != nil {
		host.setActive(false)
	}
	if cancel != nil {
		cancel()
	}
	var stopErr error
	if handler, ok := r.implementation.(Stopper); ok {
		stopCtx := context.WithoutCancel(ctx)
		stopErr = protectError("plugin.stop", func() error { return handler.OnStop(stopCtx) })
	}

	// WaitGroup 只在真实 handler 返回后归零。这里不接受调用方取消作为
	// 结束依据，否则忽略取消的 handler 会被假报为已经停止。
	r.handlerWait.Wait()

	r.mu.Lock()
	r.stopping = false
	r.mu.Unlock()
	return stopErr
}

// Unload 先完成停用排空，再调用可选的 OnUnload 并永久关闭实例。
func (r *runtime) Unload(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.mu.Lock()
	if r.unloaded {
		r.mu.Unlock()
		return nil
	}
	if !r.initialized {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.unload", "plugin instance is not initialized")
	}
	r.mu.Unlock()

	if err := contextError(ctx, "plugin.unload"); err != nil {
		return err
	}
	var stopErr error
	r.mu.Lock()
	started := r.started || r.starting
	r.mu.Unlock()
	if started {
		stopErr = r.stopLocked(ctx)
	}

	r.mu.Lock()
	cancel := r.cancelCycle
	host := r.host
	r.mu.Unlock()
	if host != nil {
		host.setActive(false)
	}
	if cancel != nil {
		cancel()
	}

	var unloadErr error
	if handler, ok := r.implementation.(Unloader); ok {
		unloadCtx := context.WithoutCancel(ctx)
		unloadErr = protectError("plugin.unload", func() error { return handler.OnUnload(unloadCtx) })
	}
	r.mu.Lock()
	r.accepting = false
	r.started = false
	r.starting = false
	r.stopping = false
	r.unloaded = true
	r.mu.Unlock()
	return errors.Join(stopErr, unloadErr)
}

// ValidateConfig 在 R05 提供严格 TOML 解码和标签校验前明确不可用。
func (r *runtime) ValidateConfig(ctx context.Context, _ []byte) error {
	if err := contextError(ctx, "plugin.config.validate"); err != nil {
		return err
	}
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.validate", "configuration validation is not implemented before R05")
}

// SetConfig 在 R05 提供发布和回调前明确不可用。
func (r *runtime) SetConfig(ctx context.Context, _ []byte) error {
	if err := contextError(ctx, "plugin.config.set"); err != nil {
		return err
	}
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.set", "configuration updates are not implemented before R05")
}

// HandleEvent 在启用周期内同步执行事件处理器。
func (r *runtime) HandleEvent(ctx context.Context, value event.Event) (event.Result, error) {
	handler, ok := r.implementation.(EventHandler)
	if !ok {
		return event.Result{}, sdk.NewError(sdk.CategoryUnimplemented, "plugin.event", "event handler is not implemented")
	}
	handlerCtx, release, err := r.beginHandler(ctx, "plugin.event")
	if err != nil {
		return event.Result{}, err
	}
	defer release()
	var result event.Result
	err = protectError("plugin.event", func() error {
		var handlerErr error
		result, handlerErr = handler.OnEvent(handlerCtx, value)
		return handlerErr
	})
	return result, err
}

// HandleCommand 在 R04 生成命令 schema 和绑定 Kong 前明确不可用。
func (r *runtime) HandleCommand(ctx context.Context, _ command.Invocation) error {
	if err := contextError(ctx, "plugin.command"); err != nil {
		return err
	}
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.command", "command handling is not implemented before R04")
}

// HandleCapability 在启用周期内同步执行能力提供者。
func (r *runtime) HandleCapability(ctx context.Context, request capability.Request) (capability.Result, error) {
	provider, ok := r.implementation.(CapabilityProvider)
	if !ok {
		return capability.Result{}, sdk.NewError(sdk.CategoryUnimplemented, "plugin.capability", "capability handler is not implemented")
	}
	if len(request.Payload) > capability.MaxInlineSize {
		return capability.Result{}, sdk.NewError(sdk.CategoryResourceExhausted, "plugin.capability", "inline payload is too large")
	}
	handlerCtx, release, err := r.beginHandler(ctx, "plugin.capability")
	if err != nil {
		return capability.Result{}, err
	}
	defer release()
	var result capability.Result
	err = protectError("plugin.capability", func() error {
		var handlerErr error
		result, handlerErr = provider.OnCapability(handlerCtx, request)
		return handlerErr
	})
	if err == nil && len(result.Payload) > capability.MaxInlineSize {
		return capability.Result{}, sdk.NewError(sdk.CategoryResourceExhausted, "plugin.capability", "inline result is too large")
	}
	return result, err
}

func (r *runtime) beginHandler(ctx context.Context, operation string) (context.Context, func(), error) {
	if err := contextError(ctx, operation); err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	if !r.initialized || !r.started || !r.accepting || r.stopping || r.unloaded {
		r.mu.Unlock()
		return nil, nil, sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin instance is not accepting work")
	}
	if r.failed != nil {
		failure := cloneBusinessError(r.failed)
		r.mu.Unlock()
		return nil, nil, failure
	}
	cycle := r.cycle
	r.handlerWait.Add(1)
	r.mu.Unlock()

	handlerCtx, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(cycle, cancel)
	release := func() {
		stopCancellation()
		cancel()
		r.handlerWait.Done()
	}
	return handlerCtx, release, nil
}

// hostLost 在反向连接失效时取消启用周期并永久关闭该实例的业务准入。
func (r *runtime) hostLost(reason *sdk.Error) {
	r.mu.Lock()
	if r.unloaded {
		r.mu.Unlock()
		return
	}
	if r.failed == nil {
		r.failed = cloneBusinessError(reason)
	}
	r.accepting = false
	cancel := r.cancelCycle
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func contextError(ctx context.Context, operation string) error {
	if ctx == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "context is nil")
	}
	switch err := ctx.Err(); {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return sdk.NewError(sdk.CategoryCanceled, operation, "operation was canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return sdk.NewError(sdk.CategoryDeadlineExceeded, operation, "operation deadline was exceeded")
	default:
		return sdk.NewError(sdk.CategoryInternal, operation, "operation context failed")
	}
}

func protectError(operation string, callback func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = sdk.NewError(sdk.CategoryInternal, operation, "plugin handler panicked")
		}
	}()
	return callback()
}

func publishCycle(implementation Plugin, cycle context.Context) {
	if publisher, ok := implementation.(interface{ publishCycle(context.Context) }); ok {
		publisher.publishCycle(cycle)
	}
}

func validateSubscriptions(subscriptions []event.Subscription) error {
	modes := make(map[event.Topic]event.Mode, len(subscriptions))
	for _, subscription := range subscriptions {
		switch subscription.Topic {
		case event.Message, event.Contact, event.Chatroom, event.Member, event.Account:
		default:
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "subscription topic is invalid")
		}
		if subscription.Mode != event.Observe && subscription.Mode != event.Consume {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "subscription mode is invalid")
		}
		if previous, exists := modes[subscription.Topic]; exists && previous != subscription.Mode {
			return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.describe", "subscription topic has conflicting modes")
		}
		modes[subscription.Topic] = subscription.Mode
	}
	return nil
}

func validateCapabilities(capabilities []capability.Spec) error {
	names := make(map[string]struct{}, len(capabilities))
	for _, specification := range capabilities {
		if strings.TrimSpace(specification.Name) == "" {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "capability name is empty")
		}
		if _, exists := names[specification.Name]; exists {
			return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.describe", "capability name is duplicated")
		}
		names[specification.Name] = struct{}{}
	}
	return nil
}

func cloneSubscriptions(values []event.Subscription) []event.Subscription {
	if values == nil {
		return nil
	}
	return slices.Clone(values)
}

func cloneCapabilities(values []capability.Spec) []capability.Spec {
	if values == nil {
		return nil
	}
	return slices.Clone(values)
}
