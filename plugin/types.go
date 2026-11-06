// Package plugin 定义插件元数据、可选生命周期接口、Host 能力和运行时契约。
package plugin

import (
	"context"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
)

// Metadata 是插件必须声明的固定元数据。
type Metadata struct {
	ID          string
	Name        string
	Description string
	Author      string
	Version     string
}

// Descriptor 是 SDK 根据实际实现自动汇总的插件描述。
//
// 插件不手写 Descriptor，也不声明自己实现了哪些可选接口。
type Descriptor struct {
	Metadata      Metadata
	Subscriptions []event.Subscription
	Commands      command.Schema
	Capabilities  []capability.Spec
	HasConfig     bool
	DefaultConfig []byte
}

// Plugin 是所有插件必须实现的最小接口。
type Plugin interface {
	Metadata() Metadata
}

// Loader 是可选加载生命周期接口。
type Loader interface {
	OnLoad(ctx context.Context) error
}

// Starter 是可选启动生命周期接口；后台任务必须从启用期 context 派生。
type Starter interface {
	OnStart(ctx context.Context) error
}

// Stopper 是可选停止生命周期接口。
type Stopper interface {
	OnStop(ctx context.Context) error
}

// Unloader 是可选卸载生命周期接口。
type Unloader interface {
	OnUnload(ctx context.Context) error
}

// EventHandler 是事件订阅和处理接口。
//
// 同一插件可以对不同主题声明 Observe 或 Consume，但同一主题不能同时声明
// 两种模式。
type EventHandler interface {
	Subscriptions() []event.Subscription
	OnEvent(ctx context.Context, value event.Event) (event.Result, error)
}

// CommandProvider 返回 SDK 用来生成命令 schema 的结构体命令树。
//
// R04 使用 Kong 从该结构体生成命令树和 schema；R02 只冻结类型和调用入口。
type CommandProvider interface {
	Commands() any
}

// CapabilityProvider 声明并处理跨插件能力。
type CapabilityProvider interface {
	Capabilities() []capability.Spec
	OnCapability(ctx context.Context, request capability.Request) (capability.Result, error)
}

// AccountClient 返回当前唯一账号快照。
type AccountClient interface {
	Current(ctx context.Context) (*sdk.Account, error)
}

// ConfigClient 是 SDK 包装的宿主配置读写入口。
//
// Read 返回当前持久化配置；Save 只能更新当前插件自己的 config 子节。
// 两者均不包含 revision、active 或 apply status。
type ConfigClient interface {
	Read(ctx context.Context) ([]byte, error)
	Save(ctx context.Context, config []byte) error
}

// ConfigReceiver 是宿主向插件运行时下发配置的入口。
//
// 实现必须严格解码、校验并发布；R05 负责完整行为。
type ConfigReceiver interface {
	SetConfig(ctx context.Context, config []byte) error
}

// Host 是插件实例绑定的宿主能力集合。
//
// 插件不保存 tenant、账号运行代次、后台 token 或 Host 路径。
type Host struct {
	Account  AccountClient
	Messages message.Client
	Contacts contact.Client
	Rooms    chatroom.Client
	Sessions session.Client
	Caller   capability.Client
	Data     Data

	state *remoteHostState
}

// NewHost 构造一组显式注入的宿主能力，供 frame 运行时和测试使用。
func NewHost(
	account AccountClient,
	messages message.Client,
	contacts contact.Client,
	rooms chatroom.Client,
	sessions session.Client,
	caller capability.Client,
	data Data,
) *Host {
	return &Host{
		Account:  account,
		Messages: messages,
		Contacts: contacts,
		Rooms:    rooms,
		Sessions: sessions,
		Caller:   caller,
		Data:     data,
	}
}

// InitializeOptions 是插件实例初始化输入。
type InitializeOptions struct {
	Host    *Host
	Config  []byte
	DataDir string
}

// Runtime 是插件运行时必须提供的最小实例契约。
//
// R03 实现生命周期和进程级单实例锁；R04 实现 Kong 命令，R05 实现配置，R06
// 实现数据目录，R07 实现日志桥接。尚未实现的方法必须返回 Unimplemented。
type Runtime interface {
	Describe(ctx context.Context) (*Descriptor, error)
	Initialize(ctx context.Context, options InitializeOptions) error
	Load(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Unload(ctx context.Context) error
	ValidateConfig(ctx context.Context, config []byte) error
	SetConfig(ctx context.Context, config []byte) error
	HandleEvent(ctx context.Context, value event.Event) (event.Result, error)
	HandleCommand(ctx context.Context, invocation command.Invocation) error
	HandleCapability(ctx context.Context, request capability.Request) (capability.Result, error)
}

const (
	// LogFieldPluginID 是宿主最终日志中可信的插件 ID 字段。
	LogFieldPluginID = "plugin_id"
	// LogFieldPluginName 是宿主最终日志中可信的插件名称字段。
	LogFieldPluginName = "plugin_name"
)
