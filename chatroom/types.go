// Package chatroom 定义群、群成员公共模型和宿主查询契约。
package chatroom

import "context"

// Chatroom 是群专属资料快照。
//
// 群在联系人视图中的昵称和备注由 contact.Contact 表达。
type Chatroom struct {
	Username      string
	Name          string
	Remark        string
	OwnerUsername string
}

// Member 是群成员快照；群消息中的实际发言者也使用该类型。
type Member struct {
	Username    string
	Chatroom    string
	DisplayName string
	Nickname    string
}

// Client 是 SDK 暴露给插件的群查询能力。
//
// Members 返回宿主缓存中的有界快照；缺失群或成员返回 NotFound，未初始化
// 返回 FailedPrecondition。
type Client interface {
	Get(ctx context.Context, username string) (*Chatroom, error)
	Members(ctx context.Context, username string) ([]Member, error)
}
