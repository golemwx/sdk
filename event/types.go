// Package event 定义插件事件和订阅契约
package event

import (
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
	"github.com/golemwx/sdk/session"
)

// SubscribeMode 订阅模式
type SubscribeMode int

const (
	// SubscribeModeUnknown 未知订阅模式
	SubscribeModeUnknown SubscribeMode = iota
	// SubscribeModeRespond 参与普通响应链，可返回消费结果
	SubscribeModeRespond
	// SubscribeModeObserve 仅观察授权事件，不影响路由
	SubscribeModeObserve
)

// Subscription 事件订阅声明
type Subscription struct {
	// Topic 订阅或投递的事件主题
	Topic string
	// Mode 事件订阅模式，区分普通响应与观察
	Mode SubscribeMode
	// Priority 订阅优先级，数值越小越优先，同值按插件 ID 稳定排序
	Priority int
}

// ChangeDomain 变更域
type ChangeDomain int

const (
	// ChangeDomainUnknown 未知资料变更域
	ChangeDomainUnknown ChangeDomain = iota
	// ChangeDomainContact 联系人资料变更
	ChangeDomainContact
	// ChangeDomainRoom 群资料变更
	ChangeDomainRoom
	// ChangeDomainRoomMember 群成员资料变更
	ChangeDomainRoomMember
	// ChangeDomainAccount 当前账号资料变更
	ChangeDomainAccount
)

// ChangeAction 变更动作
type ChangeAction int

const (
	// ChangeActionUnknown 未知变更动作
	ChangeActionUnknown ChangeAction = iota
	// ChangeActionCreated 业务对象已创建
	ChangeActionCreated
	// ChangeActionUpdated 业务对象已更新
	ChangeActionUpdated
	// ChangeActionDeleted 业务对象已删除
	ChangeActionDeleted
)

// Change 归一化资料变更
type Change struct {
	// Domain 资料变更所属业务域
	Domain ChangeDomain
	// Action 资料创建、更新或删除动作
	Action ChangeAction
	// SubjectID 本项操作或资料变更的目标业务标识
	SubjectID string
	// ParentID 父级业务标识，群成员变更时为群 username
	ParentID *string
	// ChangedFields 实际变更的 SDK 字段路径，更新时非空，缺失与显式空集合可区分
	ChangedFields []string
	// Contact 变更或查询对应的联系人业务快照
	Contact *contact.Contact
	// Room 变更或查询对应的群业务快照
	Room *chatroom.Room
	// Member 变更或查询对应的群成员业务快照
	Member *chatroom.Member
	// Account 变更或查询对应的当前账号业务快照
	Account *sdk.Account
}

// Event 投递给插件的不可变事件
type Event struct {
	// Topic 订阅或投递的事件主题
	Topic string
	// AccountUsername 所属账号的微信 username
	AccountUsername string
	// Message 关联的归一化业务消息
	Message *message.Message
	// Change 关联的资料变更，非资料变更事件时为空
	Change *Change
	// Lease 关联的会话租约
	Lease *session.Lease
	// OccurredAt 事件发生时间，必填并按 UTC 传递
	OccurredAt time.Time
}

// EventResult 事件消费结果
type EventResult struct {
	// Consumed 普通响应链是否已消费事件，观察者结果不影响路由
	Consumed bool
}
