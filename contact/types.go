// Package contact 定义联系人模型和宿主联系人客户端
package contact

import (
	"context"
	"time"

	"github.com/golemwx/sdk"
)

// ContactKind 联系人类型
type ContactKind int

const (
	// ContactKindUnknown 未知联系人类型
	ContactKindUnknown ContactKind = iota
	// ContactKindFriend 好友联系人
	ContactKindFriend
	// ContactKindRoom 群联系人
	ContactKindRoom
	// ContactKindOfficial 公众号联系人
	ContactKindOfficial
	// ContactKindSelf 账号自身
	ContactKindSelf
)

// Contact 联系人业务快照
type Contact struct {
	Username     string           // 微信 username，不另建用户标识
	Kind         ContactKind      // 业务种类，未知枚举归一为对应零值
	Nickname     *string          // 昵称，缺失与显式空字符串可区分
	Remark       *string          // 备注，缺失与显式空字符串可区分
	AvatarURL    *string          // 头像地址，缺失与显式空字符串可区分
	Alias        *string          // 微信号别名，缺失与显式空字符串可区分
	UpdatedAt    time.Time        // 资料快照更新时间，必填并按 UTC 传递
	Completeness sdk.Completeness // 对象或查询范围的资料完整性，与分页是否结束无关
}

// ContactsClient 宿主联系人能力
type ContactsClient interface {
	Get(ctx context.Context, username string) (*Contact, error)
	Find(ctx context.Context, query string, page sdk.PageRequest) (*sdk.Page[Contact], error)
	List(ctx context.Context, page sdk.PageRequest) (*sdk.Page[Contact], error)
	Refresh(ctx context.Context, usernames []string) (*sdk.BatchResult[Contact], error)
}
