package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/session"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

// authorizationContextKey 标记授权入口附加的可信上下文值
type authorizationContextKey struct{}

// TestAuthorizationPreservesParentContext 验证授权替换上下文后仍受原调用取消和截止时间约束
func TestAuthorizationPreservesParentContext(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	deadline, _ := parent.Deadline()
	interceptor := hostInterceptor(HostServices{Authorize: func(ctx context.Context, _ Invocation, _ string) (context.Context, error) {
		return context.WithValue(context.WithoutCancel(ctx), authorizationContextKey{}, "bound"), nil
	}})
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := interceptor(parent, &wire.AccountCurrentRequest{}, &grpc.UnaryServerInfo{FullMethod: wire.HostAccountService_Current_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
			boundDeadline, ok := ctx.Deadline()
			if !ok || boundDeadline.After(deadline) || ctx.Value(authorizationContextKey{}) != "bound" {
				return nil, errors.New("authorization lost deadline or bound value")
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		finished <- err
	}()
	select {
	case <-entered:
		cancel()
	case err := <-finished:
		t.Fatalf("handler did not enter: %v", err)
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation lost: %v", err)
	}
}

// renewalFixture 观察非法时长是否进入宿主业务
type renewalFixture struct {
	// fixtureSessions 复用人工租约的其他方法
	fixtureSessions
	// calls 已进入续期业务的次数
	calls int
}

// Renew 登记真实业务进入次数
func (f *renewalFixture) Renew(ctx context.Context, id string, ttl time.Duration) (*session.Lease, error) {
	f.calls++
	return f.fixtureSessions.Renew(ctx, id, ttl)
}

// TestRenewRejectsOverflow 验证 Proto 合法但 Go 无法表示的时长不会作为饱和值传给业务
func TestRenewRejectsOverflow(t *testing.T) {
	fixture := &renewalFixture{}
	server := &hostSessions{client: fixture}
	response, err := server.Renew(t.Context(), &wire.SessionRenewRequest{Ttl: &durationpb.Duration{Seconds: 315576000000}})
	if err != nil || response.GetError().GetCategory() != wire.Category_CATEGORY_INVALID_ARGUMENT || fixture.calls != 0 {
		t.Fatalf("overflow reached host: %v %v", response, err)
	}
}
