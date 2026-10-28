// Package plugin 定义插件接口、描述和宿主依赖
package plugin

import (
	"context"
	"crypto/subtle"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/config"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/contract"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
)

// 常用契约别名让插件实现只需导入 plugin 包
type (
	Permission      = sdk.Permission
	Subscription    = event.Subscription
	SubscribeMode   = event.SubscribeMode
	CommandSpec     = command.CommandSpec
	ArgSpec         = command.ArgSpec
	ArgType         = command.ArgType
	CapabilitySpec  = capability.CapabilitySpec
	Event           = event.Event
	EventResult     = event.EventResult
	Command         = command.Command
	CommandResult   = command.CommandResult
	CallRequest     = capability.CallRequest
	CallResult      = capability.CallResult
	ConfigDefaulter = config.ConfigDefaulter
	ConfigValidator = config.ConfigValidator
	ConfigApplier   = config.ConfigApplier
	MessagesClient  = message.MessagesClient
	ContactsClient  = contact.ContactsClient
	ChatroomsClient = chatroom.ChatroomsClient
	SessionsClient  = session.SessionsClient
	CallerClient    = capability.CallerClient
	ConfigClient    = config.ConfigClient
)

const (
	// SubscribeModeUnknown 未知订阅模式
	SubscribeModeUnknown = event.SubscribeModeUnknown
	// SubscribeModeRespond 参与普通响应链，可返回消费结果
	SubscribeModeRespond = event.SubscribeModeRespond
	// SubscribeModeObserve 仅观察授权事件，不影响路由
	SubscribeModeObserve = event.SubscribeModeObserve

	// ArgTypeUnknown 未知命令参数类型
	ArgTypeUnknown = command.ArgTypeUnknown
	// ArgTypeString 字符串参数
	ArgTypeString = command.ArgTypeString
	// ArgTypeInt 整数参数
	ArgTypeInt = command.ArgTypeInt
	// ArgTypeFloat 浮点数参数
	ArgTypeFloat = command.ArgTypeFloat
	// ArgTypeBool 布尔参数
	ArgTypeBool = command.ArgTypeBool
	// ArgTypeEnum 限定候选值的枚举参数
	ArgTypeEnum = command.ArgTypeEnum

	// PermissionMessageSend 发送、回复和撤回消息权限
	PermissionMessageSend = sdk.PermissionMessageSend
	// PermissionMessageRead 读取消息及下载授权媒体权限
	PermissionMessageRead = sdk.PermissionMessageRead
	// PermissionContactRead 读取联系人资料权限
	PermissionContactRead = sdk.PermissionContactRead
	// PermissionContactWrite 显式刷新联系人资料等写入口权限
	PermissionContactWrite = sdk.PermissionContactWrite
	// PermissionChatroomRead 读取群资料和成员权限
	PermissionChatroomRead = sdk.PermissionChatroomRead
	// PermissionChatroomWrite 显式刷新群资料等写入口权限
	PermissionChatroomWrite = sdk.PermissionChatroomWrite
	// PermissionSessionAcquire 申请、续期和释放自身会话租约权限
	PermissionSessionAcquire = sdk.PermissionSessionAcquire
	// PermissionCapabilityCall 按能力名调用其他插件权限
	PermissionCapabilityCall = sdk.PermissionCapabilityCall
	// PermissionConfigSelfWrite 保存插件自身业务配置权限
	PermissionConfigSelfWrite = sdk.PermissionConfigSelfWrite
	// PermissionBackgroundRun 在宿主授权范围内执行后台任务权限
	PermissionBackgroundRun = sdk.PermissionBackgroundRun
)

// Descriptor 插件固定声明
type Descriptor = contract.Descriptor

// AccountClient 宿主当前账号能力
type AccountClient interface {
	Current(ctx context.Context) (*sdk.Account, error)
}

// Host 宿主能力集合
type Host struct {
	Messages  MessagesClient  // 宿主消息发送、回复、撤回及媒体下载客户端
	Contacts  ContactsClient  // 宿主联系人查询与显式刷新客户端
	Chatrooms ChatroomsClient // 宿主群资料及成员查询与显式刷新客户端
	Sessions  SessionsClient  // 宿主联系人及整群会话租约客户端
	Caller    CallerClient    // 宿主按能力名路由的跨插件调用客户端
	Config    ConfigClient    // 宿主提供的插件自身业务配置客户端
	Account   AccountClient   // 宿主当前账号业务快照客户端

	DataDir string // 宿主分配的插件数据目录

	taskContext context.Context    // 与插件当前启用周期绑定的后台任务上下文
	taskEpoch   uint64             // SDK 内部启用周期序号，用于拒绝旧 context
	taskToken   string             // 宿主签发的不可预测后台令牌，插件不能构造或延长
	taskMu      sync.RWMutex       // 保护启用周期上下文的并发读取与替换
	taskCancel  context.CancelFunc // 结束当前启用周期
}

// NewHost 创建宿主能力集合
func NewHost(messages MessagesClient, contacts ContactsClient, chatrooms ChatroomsClient, sessions SessionsClient, caller CallerClient, configClient ConfigClient, account AccountClient, dataDir string, taskContext context.Context) *Host {
	return &Host{
		Messages:    messages,
		Contacts:    contacts,
		Chatrooms:   chatrooms,
		Sessions:    sessions,
		Caller:      caller,
		Config:      configClient,
		Account:     account,
		DataDir:     dataDir,
		taskContext: taskContext,
	}
}

// TaskContext 返回与当前启用周期绑定的上下文
func (h *Host) TaskContext() context.Context {
	if h == nil {
		return nil
	}

	h.taskMu.RLock()
	defer h.taskMu.RUnlock()
	return h.taskContext
}

// startTasks 创建不携带用户身份的新启用周期，返回仅取消本轮的句柄
func (h *Host) startTasks(parent context.Context, backgroundToken string) context.CancelFunc {
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	if h.taskCancel != nil {
		h.taskCancel()
	}
	h.taskContext, h.taskCancel = context.WithCancel(parent)
	h.taskEpoch++
	h.taskToken = backgroundToken
	h.taskContext = context.WithValue(h.taskContext, backgroundCycleKey{}, backgroundCycle{epoch: h.taskEpoch, token: backgroundToken})
	return h.taskCancel
}

// backgroundCycle 只在 SDK 内部绑定当前启用周期
type backgroundCycle struct {
	epoch uint64 // 拒绝重启用后的旧 context
	token string // 宿主签发的不可预测后台令牌
}

// backgroundCycleKey 隔离当前启用周期的私有上下文值
type backgroundCycleKey struct{}

// backgroundToken 核对派生后台上下文是否仍属于当前启用周期
func (h *Host) backgroundToken(ctx context.Context) (string, bool) {
	cycle, ok := ctx.Value(backgroundCycleKey{}).(backgroundCycle)
	if !ok || cycle.epoch == 0 || cycle.token == "" {
		return "", false
	}
	h.taskMu.RLock()
	defer h.taskMu.RUnlock()
	valid := cycle.epoch == h.taskEpoch && subtle.ConstantTimeCompare([]byte(cycle.token), []byte(h.taskToken)) == 1 &&
		h.taskContext != nil && h.taskContext.Err() == nil && ctx.Err() == nil
	return cycle.token, valid
}

// stopTasks 立即撤销后台周期，保留已取消的上下文供插件观察
func (h *Host) stopTasks() {
	if h == nil {
		return
	}
	h.taskMu.Lock()
	defer h.taskMu.Unlock()
	if h.taskCancel != nil {
		h.taskCancel()
	}
	h.taskToken = ""
}

// Plugin 插件基础契约
type Plugin interface {
	Descriptor() Descriptor
}

// Loader 加载初始化接口
type Loader interface {
	OnLoad(ctx context.Context) error
}

// Enabler 插件启用接口
type Enabler interface {
	OnEnable(ctx context.Context) error
}

// Disabler 插件禁用接口
type Disabler interface {
	OnDisable(ctx context.Context) error
}

// Unloader 插件卸载接口
type Unloader interface {
	OnUnload(ctx context.Context) error
}

// EventHandler 事件处理接口
type EventHandler interface {
	OnEvent(ctx context.Context, event Event) (EventResult, error)
}

// CommandHandler 命令处理接口
type CommandHandler interface {
	OnCommand(ctx context.Context, command Command) (CommandResult, error)
}

// CapabilityHandler 被调用能力接口
type CapabilityHandler interface {
	OnCall(ctx context.Context, request CallRequest) (CallResult, error)
}
