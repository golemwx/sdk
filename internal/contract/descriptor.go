// Package contract 存放插件声明的内部共享定义，公开入口仍为 plugin.Descriptor
package contract

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
)

// Descriptor 插件固定声明
type Descriptor struct {
	// ID 插件固定唯一标识
	ID string
	// Name 插件展示名称
	Name string
	// Description 面向使用者的业务说明
	Description string
	// Author 插件作者
	Author string
	// Version 插件展示版本，不参与握手、加载判断或能力路由
	Version string
	// RequestedPermissions 插件请求的权限，最终授权以宿主决定为准
	RequestedPermissions []sdk.Permission
	// Subscriptions 插件声明的事件订阅
	Subscriptions []event.Subscription
	// Commands 插件声明的命令集合
	Commands []command.CommandSpec
	// Capabilities 插件公开提供的能力声明
	Capabilities []capability.CapabilitySpec
	// UsesCapabilities 插件声明需要调用的能力名
	UsesCapabilities []string
}
