package capability

import (
	"context"

	"github.com/golemwx/sdk"
)

// backgroundKey 隔离后台调用目标，调用方不能伪造原用户身份
type backgroundKey struct{}

// WithBackgroundTarget 为启用期后台能力调用绑定明确的联系人或整群会话
func WithBackgroundTarget(ctx context.Context, target sdk.Conversation) context.Context {
	return context.WithValue(ctx, backgroundKey{}, target)
}

// BackgroundTarget 返回宿主用于独立后台授权的目标会话
func BackgroundTarget(ctx context.Context) (sdk.Conversation, bool) {
	target, ok := ctx.Value(backgroundKey{}).(sdk.Conversation)
	return target, ok
}
