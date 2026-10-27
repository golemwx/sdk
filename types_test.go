package sdk

import (
	"errors"
	"fmt"
	"testing"
)

// TestNormalizePageRequest 验证分页默认值、上限和非法负数
func TestNormalizePageRequest(t *testing.T) {
	tests := []struct {
		name    string // 子测试名称
		input   int    // 输入的分页条数
		want    int    // 规范化后的预期分页条数
		wantErr bool   // 是否预期拒绝输入
	}{
		{name: "default", input: 0, want: 100},
		{name: "ordinary", input: 20, want: 20},
		{name: "clamped", input: 100000, want: 1000},
		{name: "negative", input: -1, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := NormalizePageRequest("test.list", PageRequest{Limit: test.input})
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize page request: %v", err)
			}
			if result.Limit != test.want {
				t.Fatalf("limit = %d, want %d", result.Limit, test.want)
			}
		})
	}
}

// TestErrorSupportsErrorsAs 验证跨进程错误实现标准 error 接口
func TestErrorSupportsErrorsAs(t *testing.T) {
	err := NewError(CategoryNotFound, "contacts.get", "missing")
	if got := err.Error(); got != "contacts.get: missing" {
		t.Fatalf("Error() = %q", got)
	}

	var target *Error
	if !errors.As(fmt.Errorf("wrapped: %w", err), &target) || target != err {
		t.Fatal("errors.As did not recover *sdk.Error")
	}
}
