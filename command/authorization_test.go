package command

import (
	"context"
	"testing"
)

// TestAuthorizationContext 验证 owner 状态只能通过调用上下文传递。
func TestAuthorizationContext(t *testing.T) {
	if _, ok := AuthorizationFromContext(context.Background()); ok {
		t.Fatal("background context unexpectedly contains authorization")
	}

	ctx := WithAuthorization(context.Background(), Authorization{Owner: true})
	authorization, ok := AuthorizationFromContext(ctx)
	if !ok || !authorization.Owner {
		t.Fatalf("AuthorizationFromContext() = (%+v, %v)", authorization, ok)
	}
}
