package command

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/golemwx/sdk"
)

// ValidateSpec 校验单条命令声明的完整路径、参数 schema 和权限
func ValidateSpec(spec CommandSpec) error {
	if len(spec.Path) == 0 {
		return invalidSpec("command path is empty")
	}
	for _, segment := range spec.Path {
		if !validName(segment) {
			return invalidSpec("command path contains an invalid segment")
		}
	}
	if spec.Path[0] == "golem" {
		return invalidSpec("command path uses the reserved golem namespace")
	}
	if !knownPermission(spec.Permission) {
		return invalidSpec("command uses an unknown permission")
	}

	names := make(map[string]struct{}, len(spec.Args))
	for _, arg := range spec.Args {
		if !validName(arg.Name) {
			return invalidArgSpec(arg, "has an invalid name")
		}
		if _, exists := names[arg.Name]; exists {
			return invalidArgSpec(arg, "is duplicated")
		}
		names[arg.Name] = struct{}{}
		if err := validateArgSpec(arg); err != nil {
			return err
		}
	}
	return nil
}

// ValidateArgs 复制、补齐并按声明校验命名参数
func ValidateArgs(spec CommandSpec, supplied map[string]string) (map[string]string, error) {
	if err := ValidateSpec(spec); err != nil {
		return nil, err
	}

	declared := make(map[string]ArgSpec, len(spec.Args))
	for _, arg := range spec.Args {
		declared[arg.Name] = arg
	}

	validated := make(map[string]string, len(supplied)+len(spec.Args))
	for name, value := range supplied {
		if _, exists := declared[name]; !exists {
			return nil, invalidArgs("unknown command argument")
		}
		validated[name] = value
	}

	for _, arg := range spec.Args {
		value, provided := supplied[arg.Name]
		if !provided {
			switch {
			case arg.Required:
				return nil, invalidArgs("required command argument is missing")
			case arg.Default != "":
				validated[arg.Name] = arg.Default
			case arg.Type == ArgTypeBool:
				validated[arg.Name] = "false"
			}
			continue
		}
		if !validArgValue(arg, value) {
			return nil, invalidArgs("command argument has an invalid value")
		}
	}
	return validated, nil
}

// validateArgSpec 校验参数类型专属约束及非空默认值
func validateArgSpec(arg ArgSpec) error {
	switch arg.Type {
	case ArgTypeString, ArgTypeBool:
		if len(arg.Enum) != 0 || arg.Min != nil || arg.Max != nil {
			return invalidArgSpec(arg, "has constraints unsupported by its type")
		}
	case ArgTypeInt:
		if len(arg.Enum) != 0 || !validRange(arg.Min, arg.Max) || !intRangeHasValue(arg.Min, arg.Max) {
			return invalidArgSpec(arg, "has an invalid numeric range")
		}
	case ArgTypeFloat:
		if len(arg.Enum) != 0 || !validRange(arg.Min, arg.Max) {
			return invalidArgSpec(arg, "has an invalid numeric range")
		}
	case ArgTypeEnum:
		if arg.Min != nil || arg.Max != nil || len(arg.Enum) == 0 {
			return invalidArgSpec(arg, "has an invalid enum declaration")
		}
		values := make(map[string]struct{}, len(arg.Enum))
		for _, value := range arg.Enum {
			if _, exists := values[value]; exists {
				return invalidArgSpec(arg, "contains a duplicate enum value")
			}
			values[value] = struct{}{}
		}
	default:
		return invalidArgSpec(arg, "has an unknown type")
	}
	// Default 没有存在位，空字符串固定表示未声明默认值
	if arg.Default != "" && !validArgValue(arg, arg.Default) {
		return invalidArgSpec(arg, "has an invalid default value")
	}
	return nil
}

// intRangeHasValue 校验闭区间与 int64 可解析整数集合至少有一个交点
func intRangeHasValue(minimum, maximum *float64) bool {
	lower := big.NewInt(-1 << 63)
	upper := big.NewInt(1<<63 - 1)
	if minimum != nil {
		value := new(big.Rat).SetFloat64(*minimum)
		if value == nil {
			return false
		}
		candidate, remainder := new(big.Int), new(big.Int)
		candidate.QuoRem(value.Num(), value.Denom(), remainder)
		if remainder.Sign() > 0 {
			candidate.Add(candidate, big.NewInt(1))
		}
		if candidate.Cmp(lower) > 0 {
			lower = candidate
		}
	}
	if maximum != nil {
		value := new(big.Rat).SetFloat64(*maximum)
		if value == nil {
			return false
		}
		candidate, remainder := new(big.Int), new(big.Int)
		candidate.QuoRem(value.Num(), value.Denom(), remainder)
		if remainder.Sign() < 0 {
			candidate.Sub(candidate, big.NewInt(1))
		}
		if candidate.Cmp(upper) < 0 {
			upper = candidate
		}
	}
	return lower.Cmp(upper) <= 0
}

// validRange 校验浮点边界本身有限且顺序正确
func validRange(minimum, maximum *float64) bool {
	if minimum != nil && (math.IsNaN(*minimum) || math.IsInf(*minimum, 0)) {
		return false
	}
	if maximum != nil && (math.IsNaN(*maximum) || math.IsInf(*maximum, 0)) {
		return false
	}
	return minimum == nil || maximum == nil || *minimum <= *maximum
}

// validArgValue 校验单个已提供值，显式空字符串不会被当成缺失
func validArgValue(arg ArgSpec, value string) bool {
	switch arg.Type {
	case ArgTypeString:
		return true
	case ArgTypeInt:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return err == nil && intWithinRange(parsed, arg.Min, arg.Max)
	case ArgTypeFloat:
		parsed, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) && floatWithinRange(parsed, arg.Min, arg.Max)
	case ArgTypeBool:
		_, err := strconv.ParseBool(value)
		return err == nil
	case ArgTypeEnum:
		for _, candidate := range arg.Enum {
			if candidate == value {
				return true
			}
		}
	}
	return false
}

// intWithinRange 使用有理数比较，避免 int64 转 float64 后丢失边界精度
func intWithinRange(value int64, minimum, maximum *float64) bool {
	number := new(big.Rat).SetInt64(value)
	if minimum != nil && number.Cmp(new(big.Rat).SetFloat64(*minimum)) < 0 {
		return false
	}
	return maximum == nil || number.Cmp(new(big.Rat).SetFloat64(*maximum)) <= 0
}

// floatWithinRange 按声明的闭区间校验有限浮点值
func floatWithinRange(value float64, minimum, maximum *float64) bool {
	if minimum != nil && value < *minimum {
		return false
	}
	return maximum == nil || value <= *maximum
}

// validName 校验命令路径段及长选项名使用无歧义的 ASCII 名称
func validName(value string) bool {
	if value == "" || !asciiLetterOrDigit(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] != '-' && value[index] != '_' && !asciiLetterOrDigit(value[index]) {
			return false
		}
	}
	return true
}

// asciiLetterOrDigit 判断字节是否为 ASCII 字母或数字
func asciiLetterOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

// knownPermission 校验空权限或首期冻结的权限标识
func knownPermission(permission sdk.Permission) bool {
	switch permission {
	case "", sdk.PermissionMessageSend, sdk.PermissionMessageRead,
		sdk.PermissionContactRead, sdk.PermissionContactWrite,
		sdk.PermissionChatroomRead, sdk.PermissionChatroomWrite,
		sdk.PermissionSessionAcquire, sdk.PermissionCapabilityCall,
		sdk.PermissionConfigSelfWrite, sdk.PermissionBackgroundRun:
		return true
	default:
		return false
	}
}

// invalidSpec 创建命令声明错误
func invalidSpec(reason string) error {
	return sdk.NewError(sdk.CategoryInvalidArgument, "command.validate_spec", reason)
}

// invalidArgSpec 为参数 schema 错误补充稳定参数名和修复方向
func invalidArgSpec(arg ArgSpec, reason string) error {
	return invalidSpec(fmt.Sprintf("command argument %q %s", arg.Name, reason))
}

// invalidArgs 创建命令输入错误
func invalidArgs(reason string) error {
	return sdk.NewError(sdk.CategoryInvalidArgument, "command.validate_args", reason)
}
