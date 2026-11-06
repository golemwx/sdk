package plugin

import (
	"context"
	"sync"

	"github.com/golemwx/sdk"
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
	// StatusApplyFailed 表示配置已保存但运行资源应用失败。
	StatusApplyFailed
)

// Config 是插件嵌入的泛型业务配置句柄。
//
// R02 只冻结默认值、快照、Update、Save、SetConfig 和状态读取签名。
// 严格 TOML 解码、validator 校验、深拷贝、持久化、ACK 丢失对齐及
// OnConfigChanged 回调由 R05 实现。除 Snapshot 返回构造时默认值外，
// R02 的变更方法均返回 Unimplemented，不能当作可用能力。
type Config[T any] struct {
	mu       sync.RWMutex
	defaults T
	status   Status
}

// ConfigChangeHandler 是配置发布成功后的可选业务回调。
//
// previous 和 next 都是与当前配置隔离的值，插件可以按需重建运行资源。
// R05 负责调用该回调；回调不得递归调用 Update 或 Save。
type ConfigChangeHandler[T any] interface {
	OnConfigChanged(ctx context.Context, previous, next T) error
}

// NewConfig 使用业务默认值创建配置句柄。
func NewConfig[T any](defaults T) *Config[T] {
	return &Config[T]{defaults: defaults, status: StatusReady}
}

// Snapshot 返回当前配置快照。
//
// R02 尚未接入配置加载和发布，因此返回构造时的默认值。
func (c *Config[T]) Snapshot() T {
	if c == nil {
		var zero T
		return zero
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaults
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

// Update 串行修改隔离候选值；具体行为由 R05 实现。
func (c *Config[T]) Update(context.Context, func(*T) error) error {
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.update", "configuration updates are not implemented before R05")
}

// Save 整体替换候选配置；具体行为由 R05 实现。
func (c *Config[T]) Save(context.Context, T) error {
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.save", "configuration saves are not implemented before R05")
}

// SetConfig 接收宿主下发的 TOML 配置；具体行为由 R05 实现。
func (c *Config[T]) SetConfig(context.Context, []byte) error {
	return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config.set_config", "configuration injection is not implemented before R05")
}
