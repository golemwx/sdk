// Package contact 定义联系人公共模型、策略选择器和宿主查询契约。
package contact

import "context"

// Kind 表示宿主已确认的联系人种类。
type Kind uint8

const (
	// KindUnknown 表示宿主无法确认联系人种类。
	KindUnknown Kind = iota
	// KindPersonal 表示个人联系人。
	KindPersonal
	// KindRoom 表示群的会话视图。
	KindRoom
	// KindOfficial 表示公众号。
	KindOfficial
	// KindSelf 表示当前机器人账号。
	KindSelf
)

// Contact 是联系人缓存中的公共快照。
//
// 群专属字段位于 chatroom.Chatroom；群成员位于 chatroom.Member。
type Contact struct {
	Username string
	Kind     Kind
	Nickname string
	Remark   string
}

// Client 是 SDK 暴露给插件的联系人查询能力。
//
// Selector 按完整值匹配；具体检索规则见 Selector 和公共契约文档。
type Client interface {
	Find(ctx context.Context, selector Selector) (*Contact, error)
}
