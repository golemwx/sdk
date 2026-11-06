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

	commandsOnce sync.Once
	commands     *command.Set
	commandsErr  error

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
	businessErr *sdk.Error

	host        *Host
	config      configHandle
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
	config, err := discoverConfig(implementation)
	if err != nil {
		return nil, err
	}
	instance := &runtime{implementation: implementation, metadata: metadata, config: config}
	if config != nil {
		config.setOwner(implementation)
		config.setStatusListener(instance.configStatusChanged)
	}
	return instance, nil
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
			commands, err := r.commandSet()
			if err != nil {
				return err
			}
			if commands != nil {
				value.Commands = commands.Schema()
			}
			if r.config != nil {
				defaultConfig, err := r.config.defaultTOML()
				if err != nil {
					return err
				}
				value.HasConfig = true
				value.DefaultConfig = defaultConfig
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
// Initialize 绑定 Host，并通过 Host 内部配置通道装载当前配置。数据句柄
// 由 DataDir 派生，不会读取或创建该目录；调用方不能自带 Data 改变根目录。
func (r *runtime) Initialize(ctx context.Context, options InitializeOptions) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if err := contextError(ctx, "plugin.initialize"); err != nil {
		return err
	}
	r.mu.Lock()
	if r.initialized || r.unloaded {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "plugin instance is already initialized")
	}
	if options.Host == nil {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "host is nil")
	}
	if strings.TrimSpace(options.DataDir) == "" {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "data root is empty")
	}
	r.mu.Unlock()
	data, err := newDataDir(options.DataDir)
	if err != nil {
		return err
	}
	if r.config != nil {
		r.config.setClient(options.Host.configClient)
		if err := r.config.loadInitial(ctx, options.Config); err != nil {
			return err
		}
	} else if len(options.Config) > 0 {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "插件未声明配置，不能接收配置")
	}
	options.Host.Data = data
	if binder, ok := r.implementation.(interface{ bindHost(*Host) error }); ok {
		if err := binder.bindHost(options.Host); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.host = options.Host
	r.initialized = true
	r.mu.Unlock()
	return nil
}

// Load 调用可选的 OnLoad，不创建数据目录或改变 Host 连接。
func (r *runtime) Load(ctx context.Context) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	if err := contextError(ctx, "plugin.load"); err != nil {
		return err
	}
	if status := r.configStatus(); status != StatusReady {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.load", "插件配置待修复或应用失败")
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
	if status := r.configStatus(); status != StatusReady {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.start", "插件配置待修复或应用失败")
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

	if err := contextError(ctx, "plugin.unload"); err != nil {
		return err
	}
	r.mu.Lock()
	if r.unloaded {
		r.mu.Unlock()
		return nil
	}
	if !r.initialized {
		r.mu.Unlock()
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.unload", "plugin instance is not initialized")
	}
	r.stopping = true
	r.accepting = false
	started := r.started || r.starting
	r.mu.Unlock()

	var stopErr error
	if started {
		stopErr = r.stopLocked(ctx)
		r.mu.Lock()
		r.stopping = true
		r.accepting = false
		r.mu.Unlock()
	} else {
		r.handlerWait.Wait()
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

// ValidateConfig 严格解码并校验候选配置，不修改当前状态。
func (r *runtime) ValidateConfig(ctx context.Context, data []byte) error {
	if err := contextError(ctx, "plugin.config.validate"); err != nil {
		return err
	}
	if r.config == nil {
		return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.validate", "插件未声明配置")
	}
	return r.config.validateTOML(ctx, data)
}

// SetConfig 接收宿主下发的配置，校验后发布并触发可选变更回调。
func (r *runtime) SetConfig(ctx context.Context, data []byte) error {
	if err := contextError(ctx, "plugin.config.set"); err != nil {
		return err
	}
	if r.config == nil {
		return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.set", "插件未声明配置")
	}
	r.mu.Lock()
	initialized := r.initialized
	unloaded := r.unloaded
	r.mu.Unlock()
	if unloaded {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.set", "插件实例已卸载")
	}
	if !initialized {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.set", "插件实例尚未初始化")
	}
	return r.config.SetConfig(ctx, data)
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

// HandleCommand 使用 Kong 绑定命令参数、校验 selected 节点并调用 Run。
func (r *runtime) HandleCommand(ctx context.Context, invocation command.Invocation) error {
	if err := contextError(ctx, "plugin.command"); err != nil {
		return err
	}
	commands, err := r.commandSet()
	if err != nil {
		return err
	}
	if commands == nil {
		return sdk.NewError(sdk.CategoryUnimplemented, "plugin.command", "command handling is not implemented")
	}
	tokens := invocation.Tokens
	if len(tokens) == 0 && strings.TrimSpace(invocation.Raw) != "" {
		tokens, err = command.Lex(invocation.Raw)
		if err != nil {
			return err
		}
	}
	reply, err := r.commandReply(invocation)
	if err != nil {
		return err
	}
	handlerCtx, release, err := r.beginCommand(ctx, "plugin.command")
	if err != nil {
		return err
	}
	defer release()
	call, err := commands.Parse(tokens, invocation.Owner)
	if err != nil {
		return err
	}
	handlerCtx = command.WithAuthorization(handlerCtx, command.Authorization{Owner: invocation.Owner})
	if help := call.Help(); help != "" {
		return protectError("plugin.command", func() error { return reply.Text(handlerCtx, help) })
	}
	if r.configStatus() != StatusReady && !call.RequiresOwner() {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "配置待修复，仅允许 owner 命令")
	}
	return protectError("plugin.command", func() error {
		return call.Run(handlerCtx, r.implementation, reply)
	})
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
	status := r.configStatus()
	r.mu.Lock()
	if !r.initialized || !r.started || !r.accepting || r.stopping || r.unloaded {
		r.mu.Unlock()
		return nil, nil, sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin instance is not accepting work")
	}
	if r.businessErr != nil {
		failure := cloneBusinessError(r.businessErr)
		r.mu.Unlock()
		return nil, nil, failure
	}
	if status != StatusReady {
		r.mu.Unlock()
		return nil, nil, sdk.NewError(sdk.CategoryFailedPrecondition, operation, "插件配置待修复或应用失败")
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

// beginCommand 允许待配置或应用失败的插件继续执行 owner 修复命令。
func (r *runtime) beginCommand(ctx context.Context, operation string) (context.Context, func(), error) {
	if err := contextError(ctx, operation); err != nil {
		return nil, nil, err
	}
	status := r.configStatus()
	repair := status != StatusReady
	r.mu.Lock()
	if !r.initialized || r.unloaded || r.stopping {
		r.mu.Unlock()
		return nil, nil, sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin instance is not accepting work")
	}
	if r.failed != nil {
		failure := cloneBusinessError(r.failed)
		r.mu.Unlock()
		return nil, nil, failure
	}
	if !repair && (!r.started || !r.accepting) {
		r.mu.Unlock()
		return nil, nil, sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin instance is not accepting work")
	}
	cycle := r.cycle
	r.handlerWait.Add(1)
	r.mu.Unlock()

	handlerCtx, cancel := context.WithCancel(ctx)
	var stopCancellation func() bool
	if !repair && cycle != nil {
		stopCancellation = context.AfterFunc(cycle, cancel)
	}
	release := func() {
		if stopCancellation != nil {
			stopCancellation()
		}
		cancel()
		r.handlerWait.Done()
	}
	return handlerCtx, release, nil
}

func (r *runtime) configStatus() Status {
	if r.config == nil {
		return StatusReady
	}
	return r.config.configStatus()
}

func (r *runtime) configStatusChanged(status Status) {
	r.mu.Lock()
	if status == StatusApplyFailed {
		r.businessErr = sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config", "插件配置已保存，但应用失败，等待 owner 修复")
	} else {
		r.businessErr = nil
	}
	r.mu.Unlock()
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
