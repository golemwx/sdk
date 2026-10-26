// Package session 定义会话租约模型和宿主租约客户端
package session

import (
	"context"
	"time"

	"github.com/golemwx/sdk"
)

// Lease 会话租约
type Lease struct {
	// ID 由宿主签发的会话租约标识
	ID string
	// Conversation 所属或目标业务会话，群内所有成员共用整群会话
	Conversation sdk.Conversation
	// Topics 租约覆盖的事件主题，作用于联系人或整个群
	Topics []string
	// ExpiresAt 会话租约到期时间，到期后不再持有会话
	ExpiresAt time.Time
}

// SessionsClient 宿主会话租约能力
type SessionsClient interface {
	Acquire(ctx context.Context, conversation sdk.Conversation, topics []string) (*Lease, error)
	Renew(ctx context.Context, leaseID string, ttl time.Duration) (*Lease, error)
	Release(ctx context.Context, leaseID string) error
}
