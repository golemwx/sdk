package contract

import (
	"errors"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
)

// TestValidateCommandsRejectsDuplicatePath 验证同一描述内的完整命令路径不可重复
func TestValidateCommandsRejectsDuplicatePath(t *testing.T) {
	err := ValidateCommands([]command.CommandSpec{
		{Path: []string{"example", "echo"}},
		{Path: []string{"example", "echo"}},
	})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryAlreadyExists {
		t.Fatalf("duplicate command path returned unexpected error: %v", err)
	}
}
