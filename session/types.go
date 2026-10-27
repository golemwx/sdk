// Package session 定义会话租约模型和宿主租约客户端
package session

import (
	"context"
	"time"

	"github.com/golemwx/sdk"
)

// Lease 会话租约
type Lease struct {
	ID           string           // 由宿主签发的会话租约标识
	Conversation sdk.Conversation // 所属或目标业务会话，群内所有成员共用整群会话
	Topics       []string         // 租约覆盖的事件主题，作用于联系人或整个群
	ExpiresAt    time.Time        // 会话租约到期时间，到期后不再持有会话
}

// SessionsClient 宿主会话租约能力
type SessionsClient interface {
	Acquire(ctx context.Context, conversation sdk.Conversation, topics []string) (*Lease, error)
	Renew(ctx context.Context, leaseID string, ttl time.Duration) (*Lease, error)
	Release(ctx context.Context, leaseID string) error
}
