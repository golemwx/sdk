package contract

import (
	"strings"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
)

// ValidateCommands 校验同一 Descriptor 内的命令声明及完整路径唯一性
func ValidateCommands(commands []command.CommandSpec) error {
	paths := make(map[string]struct{}, len(commands))
	for _, spec := range commands {
		if err := command.ValidateSpec(spec); err != nil {
			return err
		}
		key := strings.Join(spec.Path, "\x00")
		if _, exists := paths[key]; exists {
			return sdk.NewError(sdk.CategoryAlreadyExists, "plugin.describe", "descriptor contains a duplicate command path")
		}
		paths[key] = struct{}{}
	}
	return nil
}
