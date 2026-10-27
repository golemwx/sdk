package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
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
	}}, nil)
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

// TestAuthorizationRejectsExpiredBoundContext 验证授权上下文已失效时不进入宿主业务
func TestAuthorizationRejectsExpiredBoundContext(t *testing.T) {
	for _, expired := range []bool{false, true} {
		interceptor := hostInterceptor(HostServices{Authorize: func(ctx context.Context, _ Invocation, _ string) (context.Context, error) {
			if expired {
				bound, cancel := context.WithDeadline(ctx, time.Unix(1, 0))
				t.Cleanup(cancel)
				return bound, nil
			}
			bound, cancel := context.WithCancel(ctx)
			cancel()
			return bound, nil
		}}, nil)
		called := false
		_, err := interceptor(t.Context(), &wire.AccountCurrentRequest{}, &grpc.UnaryServerInfo{FullMethod: wire.HostAccountService_Current_FullMethodName}, func(context.Context, any) (any, error) {
			called = true
			return nil, nil
		})
		category := sdk.CategoryCanceled
		if expired {
			category = sdk.CategoryDeadlineExceeded
		}
		if called || !hasCategory(transport.DecodeError(nil, err, "fixture.authorize"), category) {
			t.Fatalf("expired authorization reached host business: %v", err)
		}
	}
}

// TestInvocationAuthorityRevocation 验证超时取消与正常返回只撤销权限，不触发真实执行完成
func TestInvocationAuthorityRevocation(t *testing.T) {
	control := &hostControl{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	invocation := Invocation{ID: "issued", Token: "host-token"}
	revoke, err := control.bindInvocation(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	if _, err := control.invocationContext(Invocation{ID: "issued", Token: "forged"}); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("forged token accepted")
	}
	if _, err := control.bindInvocation(ctx, invocation); !hasCategory(err, sdk.CategoryAlreadyExists) {
		t.Fatal("duplicate live invocation accepted")
	}
	if _, err := control.invocationContext(invocation); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := control.invocationContext(invocation); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("canceled invocation retained authority")
	}
	invocation = Invocation{ID: "returned", Token: "host-token"}
	revokeReturned, err := control.bindInvocation(t.Context(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	revokeReturned()
	if _, err := control.invocationContext(invocation); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("returned invocation retained authority")
	}
}

// TestLifecycleRegistrationBindsAndRevokes 验证宿主登记先于 SDK 凭据生效，并在统一清理时一起撤销
func TestLifecycleRegistrationBindsAndRevokes(t *testing.T) {
	type contextKey struct{}
	parent := context.WithValue(t.Context(), contextKey{}, "original")
	invocation := Invocation{ID: "lifecycle", Token: "host-token"}
	registered, revoked := false, false
	control := &hostControl{}
	// 回调内无需持有 hostControl，本地凭据必须在回调成功返回后才生效
	control.register = func(ctx context.Context, received Invocation, operation LifecycleOperation) (func(), error) {
		if ctx.Value(contextKey{}) != "original" || received != invocation || operation != LifecycleEnable {
			return nil, errors.New("registration lost lifecycle context")
		}
		registered = true
		if _, err := control.invocationContext(received); err == nil {
			return nil, errors.New("SDK credential became valid before registration returned")
		}
		return func() { revoked = true }, nil
	}
	revoke, err := control.bindLifecycle(parent, invocation, LifecycleEnable)
	if err != nil || !registered {
		t.Fatalf("lifecycle registration failed: %v", err)
	}
	if bound, err := control.invocationContext(invocation); err != nil || bound.Value(contextKey{}) != "original" {
		t.Fatalf("lifecycle context was not bound: %v", err)
	}
	revoke()
	if !revoked {
		t.Fatal("host lifecycle registration was not revoked")
	}
	if _, err := control.invocationContext(invocation); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("SDK lifecycle credential remained valid")
	}
}

// TestLifecycleRegistrationRequiresRevocation 验证宿主回调不能通过遗漏撤销函数留下无期限凭据
func TestLifecycleRegistrationRequiresRevocation(t *testing.T) {
	control := &hostControl{register: func(context.Context, Invocation, LifecycleOperation) (func(), error) { return nil, nil }}
	invocation := Invocation{ID: "lifecycle", Token: "host-token"}
	if _, err := control.bindLifecycle(t.Context(), invocation, LifecycleLoad); !hasCategory(err, sdk.CategoryInternal) {
		t.Fatalf("missing revocation was accepted: %v", err)
	}
	if _, err := control.invocationContext(invocation); err == nil {
		t.Fatal("failed lifecycle registration opened SDK credential")
	}
}

// TestLifecycleCallbackPanicsAreIsolated 验证后续生命周期的宿主登记 panic 可返回，异步撤销 panic 不会带崩进程
func TestLifecycleCallbackPanicsAreIsolated(t *testing.T) {
	invocation := Invocation{ID: "lifecycle", Token: "host-token"}
	control := &hostControl{register: func(context.Context, Invocation, LifecycleOperation) (func(), error) {
		panic("private registration data")
	}}
	if _, err := control.bindLifecycle(t.Context(), invocation, LifecycleLoad); !hasCategory(err, sdk.CategoryInternal) {
		t.Fatalf("registration panic escaped: %v", err)
	}
	control.register = func(context.Context, Invocation, LifecycleOperation) (func(), error) {
		return func() { panic("private revocation data") }, nil
	}
	revoke, err := control.bindLifecycle(t.Context(), invocation, LifecycleUnload)
	if err != nil {
		t.Fatal(err)
	}
	revoke()
	if _, err := control.invocationContext(invocation); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("revocation panic retained SDK credential")
	}
}

// TestLifecycleCancellationRevokesImmediately 验证用户 handler 尚未退出时，取消已能立即撤销生命周期授权
func TestLifecycleCancellationRevokesImmediately(t *testing.T) {
	revoked := make(chan struct{})
	control := &hostControl{register: func(context.Context, Invocation, LifecycleOperation) (func(), error) {
		return func() { close(revoked) }, nil
	}}
	ctx, cancel := context.WithCancel(t.Context())
	invocation := Invocation{ID: "lifecycle", Token: "host-token"}
	revoke, err := control.bindLifecycle(ctx, invocation, LifecycleDisable)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke()
	cancel()
	guard, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	select {
	case <-revoked:
	case <-guard.Done():
		t.Fatal("cancellation did not revoke lifecycle registration")
	}
	if _, err := control.invocationContext(invocation); !hasCategory(err, sdk.CategoryPermissionDenied) {
		t.Fatal("canceled lifecycle retained SDK credential")
	}
}

// renewalFixture 观察非法时长是否进入宿主业务
type renewalFixture struct {
	fixtureSessions // 复用人工租约的其他方法

	calls int // 已进入续期业务的次数
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
