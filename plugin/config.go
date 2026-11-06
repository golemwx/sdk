package plugin

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/internal/validation"
)

// Status 是插件配置的当前可用状态。
type Status uint8

const (
	// StatusUnknown 表示配置状态未知。
	StatusUnknown Status = iota
	// StatusReady 表示配置已通过解码和校验。
	StatusReady
	// StatusPending 表示首次生成的零值配置尚待 owner 修复。
	StatusPending
	// StatusApplyFailed 表示配置已保存但运行资源应用失败，或保存结果无法确认。
	StatusApplyFailed
)

// ConfigChangeHandler 是配置发布成功后的可选业务回调。
//
// previous 和 next 都是与当前配置隔离的值，插件可以按需重建运行资源。
// 回调必须使用传入的 ctx；使用该 ctx 递归调用 Update、Save 或 SetConfig
// 会被 SDK 拒绝。
type ConfigChangeHandler[T any] interface {
	OnConfigChanged(ctx context.Context, previous, next T) error
}

// ConfigValidator 是配置标签校验之外的业务校验入口。
//
// candidate 是隔离副本，校验器不得修改当前配置。返回 nil 表示通过；
// 返回错误时 SDK 不保存、不发布候选值。
type ConfigValidator[T any] interface {
	ValidateConfig(ctx context.Context, candidate T) error
}

// Config 是插件嵌入的泛型业务配置句柄。
//
// Config 由 SDK 在 Describe 阶段上报默认对象，在 Initialize 阶段装载宿主
// 配置，并在 Update/Save 时串行修改候选值、校验、保存和发布。它不保存
// revision、active 或 apply 状态，也不直接读写配置文件。
type Config[T any] struct {
	mu       sync.RWMutex
	updateMu sync.Mutex

	defaults T
	current  T
	status   Status

	owner    Plugin
	client   ConfigClient
	listener func(Status)
}

// NewConfig 使用业务默认值创建配置句柄。
//
// 默认值会深拷贝保存，调用方后续修改原值不会影响插件配置。
func NewConfig[T any](defaults T) *Config[T] {
	return &Config[T]{
		defaults: deepCopy(defaults),
		current:  deepCopy(defaults),
		status:   StatusReady,
	}
}

// Snapshot 返回当前配置的深拷贝。
func (c *Config[T]) Snapshot() T {
	if c == nil {
		var zero T
		return zero
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return deepCopy(c.current)
}

// Status 返回当前配置可用状态。
func (c *Config[T]) Status() Status {
	if c == nil {
		return StatusUnknown
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Update 串行修改隔离候选值，校验并保存成功后自动发布。
//
// mutate 只接收候选值，不能访问 Config 的内部状态。保存失败或校验失败时
// 旧配置继续使用。保存响应丢失时会读取当前文件状态对齐，不会再次执行
// mutate。mutate 内不得调用 Update、Save 或 SetConfig。调用方必须是 frame
// 已授权的 owner 命令。
func (c *Config[T]) Update(ctx context.Context, mutate func(*T) error) error {
	if c == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.update", "配置句柄未初始化")
	}
	if err := contextError(ctx, "plugin.config.update"); err != nil {
		return err
	}
	if err := c.requireOwner(ctx, "plugin.config.update"); err != nil {
		return err
	}
	if mutate == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.update", "配置修改回调不能为空")
	}
	if err := checkConfigOperation(ctx, "plugin.config.update"); err != nil {
		return err
	}

	c.updateMu.Lock()
	defer c.updateMu.Unlock()

	previous := c.Snapshot()
	candidate := deepCopy(previous)
	if err := protectError("plugin.config.update", func() error { return mutate(&candidate) }); err != nil {
		return err
	}
	return c.saveAndPublish(ctx, previous, candidate)
}

// Save 整体替换配置，校验并保存成功后自动发布。
//
// Save 会深拷贝 candidate，调用方后续修改不会影响已发布配置。增删改命令
// 优先使用 Update，避免先 Snapshot 再 Save 覆盖并发修改。调用方必须是
// frame 已授权的 owner 命令。
func (c *Config[T]) Save(ctx context.Context, candidate T) error {
	if c == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.save", "配置句柄未初始化")
	}
	if err := contextError(ctx, "plugin.config.save"); err != nil {
		return err
	}
	if err := c.requireOwner(ctx, "plugin.config.save"); err != nil {
		return err
	}
	if err := checkConfigOperation(ctx, "plugin.config.save"); err != nil {
		return err
	}

	c.updateMu.Lock()
	defer c.updateMu.Unlock()

	previous := c.Snapshot()
	return c.saveAndPublish(ctx, previous, deepCopy(candidate))
}

// SetConfig 接收宿主下发的 TOML 配置并发布。
//
// SetConfig 用于启动装载和 `/pm reload`，不再次保存文件，也不要求 owner
// 上下文。严格解码、标签校验和业务校验全部通过后才替换当前配置。
func (c *Config[T]) SetConfig(ctx context.Context, data []byte) error {
	if c == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.set", "配置句柄未初始化")
	}
	if err := contextError(ctx, "plugin.config.set"); err != nil {
		return err
	}
	if err := checkConfigOperation(ctx, "plugin.config.set"); err != nil {
		return err
	}

	c.updateMu.Lock()
	defer c.updateMu.Unlock()

	candidate, err := decodeConfig[T](data)
	if err != nil {
		return err
	}
	if err := c.validateValue(ctx, candidate); err != nil {
		return err
	}
	return c.publish(ctx, c.Snapshot(), candidate)
}

func (c *Config[T]) saveAndPublish(ctx context.Context, previous, candidate T) error {
	client := c.configClient()
	if client == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.save", "配置保存通道未连接")
	}
	if err := c.validateValue(ctx, candidate); err != nil {
		return err
	}
	encoded, err := encodeConfig(candidate)
	if err != nil {
		return err
	}
	if err := c.checkFileUnchanged(ctx, client, previous); err != nil {
		return err
	}

	saveErr := client.Save(ctx, encoded)
	if saveErr != nil {
		if !isUnknownWrite(saveErr) {
			return saveErr
		}
		aligned, pause, alignErr := c.alignAfterUnknown(ctx, client, previous, candidate)
		if alignErr != nil {
			if pause {
				c.setStatus(StatusApplyFailed)
			}
			return alignErr
		}
		if !aligned {
			return saveErr
		}
	}
	return c.publish(ctx, previous, candidate)
}

func (c *Config[T]) checkFileUnchanged(ctx context.Context, client ConfigClient, current T) error {
	data, err := client.Read(ctx)
	if err != nil {
		if isUnimplemented(err) {
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	fileValue, err := decodeConfig[T](data)
	if err != nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.save", "当前配置文件无法解析，请先 reload")
	}
	if !configEquivalent(fileValue, current) {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.save", "配置文件已被外部修改，请先 reload")
	}
	return nil
}

// alignAfterUnknown 在保存响应丢失后读取当前文件状态。
//
// aligned=true 表示文件已经是候选值，可以发布但不再执行修改回调；
// pause=true 表示无法确认或文件与当前值、候选值都不一致，应暂停业务。
func (c *Config[T]) alignAfterUnknown(ctx context.Context, client ConfigClient, previous, candidate T) (aligned, pause bool, err error) {
	data, readErr := client.Read(ctx)
	if readErr != nil {
		return false, true, sdk.NewError(
			sdk.CategoryFailedPrecondition,
			"plugin.config.save",
			"保存响应丢失，且无法读取当前配置，插件业务已暂停，请重载",
		)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return false, false, nil
	}
	fileValue, decodeErr := decodeConfig[T](data)
	if decodeErr != nil {
		return false, true, sdk.NewError(
			sdk.CategoryFailedPrecondition,
			"plugin.config.save",
			"保存响应丢失，且当前配置无法解析，插件业务已暂停，请重载",
		)
	}
	if configEquivalent(fileValue, candidate) {
		return true, false, nil
	}
	if configEquivalent(fileValue, previous) {
		return false, false, nil
	}
	return false, true, sdk.NewError(
		sdk.CategoryFailedPrecondition,
		"plugin.config.save",
		"保存响应丢失，且配置文件已被外部修改，请先 reload",
	)
}

func (c *Config[T]) publish(ctx context.Context, previous, next T) error {
	c.mu.Lock()
	c.current = deepCopy(next)
	oldStatus := c.status
	c.status = StatusReady
	c.mu.Unlock()
	if oldStatus != StatusReady {
		c.notifyStatus(StatusReady)
	}
	return c.apply(ctx, previous, next)
}

func (c *Config[T]) apply(ctx context.Context, previous, next T) error {
	c.mu.RLock()
	owner := c.owner
	c.mu.RUnlock()
	handler, ok := owner.(ConfigChangeHandler[T])
	if !ok || handler == nil {
		return nil
	}

	err := protectError("plugin.config.apply", func() error {
		return handler.OnConfigChanged(markConfigOperation(ctx), deepCopy(previous), deepCopy(next))
	})
	if err == nil {
		return nil
	}
	c.setStatus(StatusApplyFailed)
	applied := sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.apply", "配置已保存，但应用失败")
	applied.Outcome = sdk.OutcomeSucceeded
	return applied
}

func (c *Config[T]) setStatus(status Status) {
	c.mu.Lock()
	oldStatus := c.status
	c.status = status
	c.mu.Unlock()
	if oldStatus != status {
		c.notifyStatus(status)
	}
}

func (c *Config[T]) notifyStatus(status Status) {
	c.mu.RLock()
	listener := c.listener
	c.mu.RUnlock()
	if listener != nil {
		listener(status)
	}
}

func (c *Config[T]) loadInitial(ctx context.Context, data []byte) error {
	c.updateMu.Lock()
	defer c.updateMu.Unlock()

	if len(bytes.TrimSpace(data)) == 0 {
		defaults := c.defaultsSnapshot()
		if err := c.validateValue(ctx, deepCopy(defaults)); err != nil {
			c.publishInitial(defaults, StatusPending)
			return nil
		}
		c.publishInitial(defaults, StatusReady)
		return nil
	}

	candidate, err := decodeConfig[T](data)
	if err != nil {
		return err
	}
	if err := c.validateValue(ctx, candidate); err != nil {
		defaults := c.defaultsSnapshot()
		defaultsErr := c.validateValue(ctx, deepCopy(defaults))
		if defaultsErr != nil && configEquivalent(candidate, defaults) {
			c.publishInitial(candidate, StatusPending)
			return nil
		}
		return err
	}
	c.publishInitial(candidate, StatusReady)
	return nil
}

func (c *Config[T]) publishInitial(value T, status Status) {
	c.mu.Lock()
	c.current = deepCopy(value)
	c.status = status
	c.mu.Unlock()
	c.notifyStatus(status)
}

func (c *Config[T]) validateValue(ctx context.Context, candidate T) error {
	if isStructLike(candidate) {
		if err := validation.Struct(candidate, "plugin.config.validate"); err != nil {
			return err
		}
	}
	c.mu.RLock()
	owner := c.owner
	c.mu.RUnlock()
	validator, ok := owner.(ConfigValidator[T])
	if !ok || validator == nil {
		return nil
	}
	return protectError("plugin.config.validate", func() error {
		return validator.ValidateConfig(markConfigOperation(ctx), deepCopy(candidate))
	})
}

func (c *Config[T]) validateTOML(ctx context.Context, data []byte) error {
	if c == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.validate", "配置句柄未初始化")
	}
	if err := contextError(ctx, "plugin.config.validate"); err != nil {
		return err
	}
	candidate, err := decodeConfig[T](data)
	if err != nil {
		return err
	}
	return c.validateValue(ctx, candidate)
}

func (c *Config[T]) defaultTOML() ([]byte, error) {
	if c == nil {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.config.describe", "配置句柄未初始化")
	}
	c.mu.RLock()
	defaults := deepCopy(c.defaults)
	c.mu.RUnlock()
	return encodeConfig(defaults)
}

func (c *Config[T]) configStatus() Status {
	if c == nil {
		return StatusUnknown
	}
	return c.Status()
}

func (c *Config[T]) setOwner(owner Plugin) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
}

func (c *Config[T]) setClient(client ConfigClient) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()
}

func (c *Config[T]) setStatusListener(listener func(Status)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.listener = listener
	c.mu.Unlock()
}

func (c *Config[T]) configClient() ConfigClient {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

func (c *Config[T]) defaultsSnapshot() T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return deepCopy(c.defaults)
}

func (c *Config[T]) requireOwner(ctx context.Context, operation string) error {
	authorization, ok := command.AuthorizationFromContext(ctx)
	if !ok || !authorization.Owner {
		return sdk.NewError(sdk.CategoryPermissionDenied, operation, "只有 owner 可以修改插件配置")
	}
	return nil
}

type configOperationKey struct{}

type configOperation struct{}

// markConfigOperation 标记 SDK 主动调用的业务回调上下文。
//
// 使用该上下文再次进入 Update/Save/SetConfig 会被 checkConfigOperation
// 立即拒绝；其他 goroutine 的并发调用不带标记，会正常排队等待更新锁。
func markConfigOperation(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, configOperationKey{}, configOperation{})
}

func checkConfigOperation(ctx context.Context, operation string) error {
	if ctx == nil {
		return nil
	}
	if _, ok := ctx.Value(configOperationKey{}).(configOperation); ok {
		return sdk.NewError(sdk.CategoryFailedPrecondition, operation, "配置回调不能递归修改配置")
	}
	return nil
}

func isUnknownWrite(err error) bool {
	var business *sdk.Error
	if errors.As(err, &business) {
		return business.Outcome == sdk.OutcomeUnknown
	}
	return true
}

func isUnimplemented(err error) bool {
	var business *sdk.Error
	return errors.As(err, &business) && business.Category == sdk.CategoryUnimplemented
}

func isStructLike(value any) bool {
	if value == nil {
		return false
	}
	typ := reflect.TypeOf(value)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.Struct
}

var _ ConfigReceiver = (*Config[any])(nil)
