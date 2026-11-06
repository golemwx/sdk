package plugin

import "context"

// Base 保存单个插件实例的宿主句柄和启用期 context。
//
// 插件应嵌入 NewBase 返回的指针。R03 在初始化成功后发布 Host，在生命周期
// 开始后发布 Context；R02 不实现绑定、复制保护或连接清理。
type Base struct {
	Host *Host
	ctx  context.Context
}

// NewBase 创建尚未绑定的 Base。
func NewBase() *Base {
	return &Base{}
}

// Context 返回当前插件实例的启用期 context。
//
// 初始化前返回 nil；R03 负责在正确生命周期阶段写入。
func (b *Base) Context() context.Context {
	if b == nil {
		return nil
	}
	return b.ctx
}
