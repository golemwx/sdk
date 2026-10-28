package transport

import (
	"context"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
)

const (
	// ControlInvocationHeader 为空请求的配置默认值查询携带宿主执行标识
	ControlInvocationHeader = "golem-control-invocation-id"
	// LifecycleInvocationIDHeader 在受保护的正向调用中传递宿主签发的生命周期标识
	LifecycleInvocationIDHeader = "golem-lifecycle-invocation-id"
	// LifecycleInvocationTokenHeader 仅在对应生命周期调用存活期间传递单次凭据
	LifecycleInvocationTokenHeader = "golem-lifecycle-invocation-token"
)

// invocationKey 隔离 SDK 内部凭据，业务代码不能通过公开字段替换
type invocationKey struct{}

type backgroundConversationKey struct{}

type backgroundTokenKey struct{}

// invocationValue 仅保存凭据值，不把生成消息中的同步状态放入 context
type invocationValue struct {
	id    string // 宿主签发的执行标识
	token string // 宿主签发的单次凭据
}

// WithInvocation 在原调用上下文上绑定宿主签发的身份，不改变取消与期限
func WithInvocation(ctx context.Context, id, token string) context.Context {
	return context.WithValue(ctx, invocationKey{}, invocationValue{id: id, token: token})
}

// InvocationFromContext 复制当前用户调用凭据；独立后台入口不使用该字段
func InvocationFromContext(ctx context.Context) *wire.Invocation {
	value, ok := ctx.Value(invocationKey{}).(invocationValue)
	if !ok {
		return nil
	}
	return &wire.Invocation{Id: value.id, Token: value.token}
}

// WithBackgroundConversation 在宿主服务端绑定独立后台授权目标
func WithBackgroundConversation(ctx context.Context, target sdk.Conversation) context.Context {
	return context.WithValue(ctx, backgroundConversationKey{}, target)
}

// BackgroundConversation 返回宿主服务端已转换的后台目标会话
func BackgroundConversation(ctx context.Context) (sdk.Conversation, bool) {
	target, ok := ctx.Value(backgroundConversationKey{}).(sdk.Conversation)
	return target, ok
}

// WithBackgroundToken 在宿主服务端绑定当前启用周期的后台令牌
func WithBackgroundToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, backgroundTokenKey{}, token)
}

// BackgroundToken 返回宿主服务端从后台请求中提取的令牌
func BackgroundToken(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(backgroundTokenKey{}).(string)
	return token, ok && token != ""
}
