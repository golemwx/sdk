// Package session 定义联系人或整个群的事件消费租约。
package session

import (
	"context"
	"time"

	"github.com/golemwx/sdk/event"
)

// Lease 是宿主签发的会话租约。
//
// Target 是联系人或群 username，租约不按群成员拆分。
type Lease struct {
	ID        string
	Target    string
	Topics    []event.Topic
	ExpiresAt time.Time
}

// Client 是宿主会话租约能力。
type Client interface {
	Acquire(ctx context.Context, target string, topics []event.Topic) (*Lease, error)
	Renew(ctx context.Context, leaseID string, ttl time.Duration) (*Lease, error)
	Release(ctx context.Context, leaseID string) error
}
