package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/golemwx/sdk"
)

type fixtureConfig struct {
	Threshold int
}

var _ ConfigReceiver = (*Config[fixtureConfig])(nil)

// TestConfigContractIsExplicitlyUnimplemented 验证 R02 不伪造配置保存能力。
func TestConfigContractIsExplicitlyUnimplemented(t *testing.T) {
	config := NewConfig(fixtureConfig{Threshold: 2})
	if got := config.Snapshot().Threshold; got != 2 {
		t.Fatalf("Snapshot() Threshold = %d, want 2", got)
	}
	if got := config.Status(); got != StatusReady {
		t.Fatalf("Status() = %v, want StatusReady", got)
	}

	calls := []struct {
		name string
		call func() error
	}{
		{name: "Update", call: func() error {
			return config.Update(context.Background(), func(*fixtureConfig) error { return nil })
		}},
		{name: "Save", call: func() error {
			return config.Save(context.Background(), fixtureConfig{Threshold: 3})
		}},
		{name: "SetConfig", call: func() error {
			return config.SetConfig(context.Background(), []byte("threshold = 3"))
		}},
	}
	for _, test := range calls {
		t.Run(test.name, func(t *testing.T) {
			var business *sdk.Error
			if err := test.call(); !errors.As(err, &business) || business.Category != sdk.CategoryUnimplemented {
				t.Fatalf("%s error = %v, want Unimplemented", test.name, err)
			}
		})
	}
}
