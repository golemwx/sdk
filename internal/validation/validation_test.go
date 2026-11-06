package validation

import (
	"errors"
	"strings"
	"testing"

	"github.com/golemwx/sdk"
)

type validationConfig struct {
	Threshold int    `toml:"threshold" validate:"min=2,max=10"`
	Window    string `toml:"window" validate:"required,duration"`
	Schedule  string `toml:"schedule" validate:"omitempty,cron"`
}

// TestStructUsesPublicFieldNames 验证错误使用 TOML 字段并输出中文原因。
func TestStructUsesPublicFieldNames(t *testing.T) {
	err := Struct(validationConfig{Threshold: 1, Window: "later", Schedule: "bad"}, "plugin.config.validate")
	if err == nil {
		t.Fatal("Struct() unexpectedly succeeded")
	}
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
		t.Fatalf("Struct() error = %v, want InvalidArgument", err)
	}
	for _, want := range []string{"threshold", "window", "schedule", "不能小于 2", "不是有效的时长", "不是有效的 Cron 表达式"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Struct() error = %q, want %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "validationConfig") {
		t.Fatalf("Struct() error leaked Go type path: %q", err)
	}
}

// TestStructAcceptsValidDurationAndCron 验证通用规则接受合法值。
func TestStructAcceptsValidDurationAndCron(t *testing.T) {
	if err := Struct(validationConfig{Threshold: 5, Window: "30s", Schedule: "0 8 * * *"}, "test"); err != nil {
		t.Fatalf("Struct() error = %v", err)
	}
}
