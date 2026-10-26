package transport

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/wire"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TestBusinessStatusRoundTrip 验证通用拦截器不会丢失业务错误类别和重试提示
func TestBusinessStatusRoundTrip(t *testing.T) {
	for category := sdk.CategoryInvalidArgument; category <= sdk.CategoryInternal; category++ {
		input := sdk.NewError(category, "fixture.operation", "sanitized reason")
		input.InvocationID, input.RetryAfter, input.Outcome = "invocation", time.Second, sdk.OutcomeUnknown
		decoded := DecodeError(nil, RPCError(input, "fixture.operation"), "caller.operation")
		var result *sdk.Error
		if !errors.As(decoded, &result) || *result != *input {
			t.Fatalf("category %v changed: %#v", category, result)
		}
	}
}

// TestRawContextErrors 验证非 gRPC 包装的父调用取消及超时仍保留正确类别
func TestRawContextErrors(t *testing.T) {
	for _, source := range []error{context.Canceled, context.DeadlineExceeded} {
		var business *sdk.Error
		if !errors.As(DecodeError(nil, fmt.Errorf("wrapped: %w", source), "fixture.read"), &business) {
			t.Fatal("missing business error")
		}
		want := sdk.CategoryCanceled
		if errors.Is(source, context.DeadlineExceeded) {
			want = sdk.CategoryDeadlineExceeded
		}
		if business.Category != want {
			t.Fatalf("context error became %v, want %v", business.Category, want)
		}
	}
}

// TestMalformedWriteError 保证损坏错误详情不会把可能已生效的写操作降为只读结果
func TestMalformedWriteError(t *testing.T) {
	value := &wire.Error{RetryAfter: &durationpb.Duration{Seconds: 315576000000}}
	var business *sdk.Error
	if !errors.As(DecodeError(value, nil, "messages.send_text"), &business) || business.Category != sdk.CategoryInternal || business.Outcome != sdk.OutcomeUnknown {
		t.Fatalf("malformed write result changed outcome: %v", business)
	}
}

// TestTransportWriteOutcome 验证传输失败及缺失回执不会误报确定写入结果
func TestTransportWriteOutcome(t *testing.T) {
	for _, operation := range []string{"messages.send_text", "messages.reply", "messages.revoke"} {
		for _, err := range []error{status.Error(codes.DeadlineExceeded, "timeout"), status.Error(codes.Canceled, "canceled"), status.Error(codes.Internal, "response damaged")} {
			var business *sdk.Error
			if !errors.As(DecodeError(nil, err, operation), &business) || business.Outcome != sdk.OutcomeUnknown {
				t.Fatalf("write outcome changed: %v", business)
			}
		}
		var missing *sdk.Error
		if !errors.As(ResultError(false, nil, nil, operation), &missing) || missing.Category != sdk.CategoryInternal || missing.Outcome != sdk.OutcomeUnknown {
			t.Fatalf("missing receipt: %v", missing)
		}
	}
}

// TestErrorSanitization 验证普通插件错误不泄漏私有文本及只读取消保持类别
func TestErrorSanitization(t *testing.T) {
	encoded := EncodeError(errors.New("private credentials"), "fixture.read")
	if encoded.GetReason() == "private credentials" {
		t.Fatal("private error leaked")
	}
	decoded := DecodeError(EncodeError(context.Canceled, "fixture.read"), nil, "fixture.read")
	var business *sdk.Error
	if !errors.As(decoded, &business) || business.Category != sdk.CategoryCanceled || business.Outcome != sdk.OutcomeNotApplicable {
		t.Fatalf("cancel projection: %v", decoded)
	}
}
