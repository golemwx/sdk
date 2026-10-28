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
	ID                   string                      // 插件固定唯一标识
	Name                 string                      // 插件展示名称
	Description          string                      // 面向使用者的业务说明
	Author               string                      // 插件作者
	Version              string                      // 插件展示版本，不参与握手、加载判断或能力路由
	RequestedPermissions []sdk.Permission            // 插件请求的权限，最终授权以宿主决定为准
	Subscriptions        []event.Subscription        // 插件声明的事件订阅
	Commands             []command.CommandSpec       // 插件声明的命令集合
	Capabilities         []capability.CapabilitySpec // 插件公开提供的能力声明
	UsesCapabilities     []string                    // 插件声明需要调用的能力名
	HasConfigDefaults    bool                        // 插件是否实现 ConfigDefaulter
	HasConfigValidate    bool                        // 插件是否实现 ConfigValidator
	HasConfigApply       bool                        // 插件是否实现 ConfigApplier
}
