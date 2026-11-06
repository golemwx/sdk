package plugin

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/golemwx/sdk"
)

// Base 保存单个插件实例的宿主句柄和启用期 context。
//
// 插件必须嵌入 NewBase 返回的指针，且不得复制或复用到另一个实例。SDK 在
// Initialize 成功后发布 Host，在 Start 成功后发布 Context；Host 字段一旦发布
// 即使停用或断连也保留，但其中的远程能力会明确失败。
type Base struct {
	Host *Host

	state *baseState
}

type baseState struct {
	mu    sync.Mutex
	bound bool
	cycle atomic.Pointer[cycleContext]
}

type cycleContext struct {
	value context.Context
}

// NewBase 创建尚未绑定的 Base。
func NewBase() *Base {
	return &Base{state: &baseState{}}
}

// Context 返回当前插件实例的启用期 context。
//
// 初始化前返回 nil；R03 负责在正确生命周期阶段写入。
func (b *Base) Context() context.Context {
	if b == nil {
		return nil
	}
	state := b.state
	if state == nil {
		return nil
	}
	cycle := state.cycle.Load()
	if cycle == nil {
		return nil
	}
	return cycle.value
}

// bindHost 仅由 SDK 在首次成功初始化时调用。
func (b *Base) bindHost(host *Host) error {
	if b == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "base receiver is nil")
	}
	if host == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "host is nil")
	}
	if b.state == nil {
		b.state = &baseState{}
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.bound {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "base is already bound")
	}
	b.state.bound = true
	b.Host = host
	return nil
}

// publishCycle 在启用回调执行前发布新的周期 context。
func (b *Base) publishCycle(ctx context.Context) {
	if b == nil || b.state == nil {
		return
	}
	b.state.cycle.Store(&cycleContext{value: ctx})
}
