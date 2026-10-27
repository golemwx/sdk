package plugin

import (
	"sync/atomic"

	"github.com/golemwx/sdk"
)

// Base 为插件实例保存 SDK 自动绑定的宿主能力
//
// 插件通常按值嵌入零值 Base，并始终以指针形式交给 Serve
// Base 首次使用后不得复制或复用到其他连接
// Host 仅在初始化后的回调中读取，不得改写；失效后仍保留已关闭的句柄
type Base struct {
	Host  *Host                     // SDK 在 Initialize 中写入一次的稳定宿主句柄
	state atomic.Pointer[baseState] // 当前不可变绑定的原子引用
}

// baseState 是一次不可变的实例级宿主绑定
type baseState struct {
	binding *hostBinding // 标识建立该引用的连接代次
}

// hostBinding 标识一次私有宿主绑定，防止旧连接清理新引用
type hostBinding struct {
	marker byte // 确保不同连接的指针身份不会因零尺寸对象而重合
}

// hostBinder 仅允许 SDK 通过嵌入 Base 的提升方法管理宿主引用
type hostBinder interface {
	bindHost(host *Host, binding *hostBinding) error
	unbindHost(binding *hostBinding)
}

// NewBase 创建注入本地假 Host 的 Base，供不启动传输层的测试使用
func NewBase(host *Host) *Base {
	base := &Base{}
	if host != nil {
		base.Host = host
		base.state.Store(&baseState{binding: &hostBinding{}})
	}
	return base
}

// bindHost 原子建立正式实例绑定，拒绝覆盖本地假 Host 或现有连接
func (b *Base) bindHost(host *Host, binding *hostBinding) error {
	if b == nil || host == nil || binding == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "invalid host binding")
	}
	if !b.state.CompareAndSwap(nil, &baseState{binding: binding}) {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "host is already bound")
	}
	// 仅在业务回调开放前发布字段；失效时保留句柄，避免与并发读取竞争
	b.Host = host
	return nil
}

// unbindHost 仅撤销指定连接代次的绑定，保留稳定字段并禁止重复使用 Base
func (b *Base) unbindHost(binding *hostBinding) {
	if b == nil || binding == nil {
		return
	}
	for {
		state := b.state.Load()
		if state == nil || state.binding != binding {
			return
		}
		if b.state.CompareAndSwap(state, &baseState{}) {
			return
		}
	}
}
