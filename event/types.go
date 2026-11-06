// Package event 定义瞬时事件、订阅模式和消费结果。
package event

import (
	"time"

	"github.com/golemwx/sdk/chatroom"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
)

// Topic 是事件主题标识。
type Topic string

const (
	// Message 是完整消息事件主题。
	Message Topic = "message"
	// Contact 是联系人资料变化主题。
	Contact Topic = "contact"
	// Chatroom 是群资料变化主题。
	Chatroom Topic = "chatroom"
	// Member 是群成员资料变化主题。
	Member Topic = "member"
	// Account 是当前账号资料变化主题。
	Account Topic = "account"
)

// Mode 是插件对主题的订阅模式。
type Mode uint8

const (
	// ModeUnknown 表示订阅模式未知。
	ModeUnknown Mode = iota
	// Observe 表示独立观察，不影响消费链和会话租约。
	Observe
	// Consume 表示参与消费链，可返回 Result.Consumed。
	Consume
)

// Subscription 是插件声明的单个事件订阅。
//
// Priority 仅对消费者有意义：数值越小越先执行，同值按插件 ID 稳定排序。
type Subscription struct {
	Topic    Topic
	Mode     Mode
	Priority int
}

// Event 是一次函数调用的只读输入，插件不得修改其中的共享快照。
//
// 单账号系统不在事件中携带 tenant、账号 username、epoch 或事件序号。
// Account 主题不重复携带账号快照；处理器应通过 Host.Account.Current 读取。
type Event struct {
	Topic      Topic
	Message    *message.Message
	Contact    *contact.Contact
	Chatroom   *chatroom.Chatroom
	Member     *chatroom.Member
	OccurredAt time.Time
}

// Result 是消费者返回的消费结果；观察者返回值被忽略。
type Result struct {
	Consumed bool
}
