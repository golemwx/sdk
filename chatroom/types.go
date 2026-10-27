// Package chatroom 定义群、成员模型和宿主群客户端
package chatroom

import (
	"context"
	"time"

	"github.com/golemwx/sdk"
)

// Room 群业务快照
type Room struct {
	Username            string           // 微信 username，不另建用户标识
	Name                *string          // 群名称，缺失与显式空字符串可区分
	Remark              *string          // 备注，缺失与显式空字符串可区分
	OwnerUsername       *string          // 群主的微信 username，未知时缺失
	Announcement        *string          // 群公告，缺失与显式空字符串可区分
	AnnouncementVersion *int32           // 修改群公告时原样回传的并发前置版本，未知时缺失
	MemberCount         *int32           // 群成员数量，未知时缺失，显式 0 与缺失可区分
	UpdatedAt           time.Time        // 资料快照更新时间，必填并按 UTC 传递
	Completeness        sdk.Completeness // 对象或查询范围的资料完整性，与分页是否结束无关
}

// MemberRole 群成员角色
type MemberRole int

const (
	// MemberRoleUnknown 群成员角色未知，不得据此授予管理权限
	MemberRoleUnknown MemberRole = iota
	// MemberRoleOwner 已确认的群主
	MemberRoleOwner
	// MemberRoleMember 普通群成员
	MemberRoleMember
	// MemberRoleAdmin 已确认的群管理员，首期没有可靠来源时不产出
	MemberRoleAdmin
)

// Member 群成员业务快照
type Member struct {
	RoomUsername string           // 群的微信 username
	UserUsername string           // 群成员的微信 username
	DisplayName  *string          // 展示名称，缺失与显式空字符串可区分
	Nickname     *string          // 昵称，缺失与显式空字符串可区分
	Role         MemberRole       // 已确认的群成员角色，未知角色不得用于授予管理权限
	JoinedAt     *time.Time       // 入群时间，未知时缺失，有值时按 UTC 传递
	Completeness sdk.Completeness // 对象或查询范围的资料完整性，与分页是否结束无关
}

// ChatroomRefreshResult 群刷新结果
type ChatroomRefreshResult struct {
	Room    *Room                // 变更或查询对应的群业务快照
	Members *sdk.Page[Member]    // 群成员分页结果
	Stages  []RefreshStageResult // 各上游只读刷新阶段的独立结果
}

// RefreshStageResult 单个上游只读阶段的结果
type RefreshStageResult struct {
	Name    string           // 上游只读刷新阶段名称
	Outcome sdk.WriteOutcome // 目标写入副作用结果，只读操作使用 NotApplicable
	Error   *sdk.Error       // 业务错误，成功时为空；部分成功仍保留逐项结果
}

// ChatroomsClient 宿主群与成员能力
type ChatroomsClient interface {
	Get(ctx context.Context, roomUsername string) (*Room, error)
	Members(ctx context.Context, roomUsername string, page sdk.PageRequest) (*sdk.Page[Member], error)
	Member(ctx context.Context, roomUsername, userUsername string) (*Member, error)
	Refresh(ctx context.Context, roomUsername string) (*ChatroomRefreshResult, error)
}
