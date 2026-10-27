package command

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/golemwx/sdk"
)

// numberPointer 创建命令范围测试使用的独立边界
func numberPointer(value float64) *float64 { return &value }

// requireCategory 校验命令辅助返回可机器判断的业务错误
func requireCategory(t *testing.T, err error, category sdk.Category) {
	t.Helper()
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != category {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateSpec 验证命令路径、参数 schema、默认值及权限声明
func TestValidateSpec(t *testing.T) {
	valid := CommandSpec{
		Path:       []string{"example", "run-task"},
		Permission: sdk.PermissionMessageSend,
		Args: []ArgSpec{
			{Name: "text", Type: ArgTypeString, Required: true},
			{Name: "count", Type: ArgTypeInt, Default: "2", Min: numberPointer(1), Max: numberPointer(3)},
			{Name: "ratio", Type: ArgTypeFloat, Default: "0.5", Min: numberPointer(0), Max: numberPointer(1)},
			{Name: "enabled", Type: ArgTypeBool, Default: "true"},
			{Name: "mode", Type: ArgTypeEnum, Default: "fast", Enum: []string{"fast", "safe"}},
		},
	}
	if err := ValidateSpec(valid); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}

	tests := []struct {
		name string
		spec CommandSpec
	}{
		{name: "missing path", spec: CommandSpec{}},
		{name: "empty path segment", spec: CommandSpec{Path: []string{"example", ""}}},
		{name: "ambiguous path segment", spec: CommandSpec{Path: []string{"example/run"}}},
		{name: "reserved namespace", spec: CommandSpec{Path: []string{"golem", "help"}}},
		{name: "unknown permission", spec: CommandSpec{Path: []string{"example"}, Permission: "unknown.permission"}},
		{name: "invalid argument name", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "bad name", Type: ArgTypeString}}}},
		{name: "duplicate argument", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeString}, {Name: "value", Type: ArgTypeString}}}},
		{name: "unknown argument type", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgType(99)}}}},
		{name: "string range", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeString, Min: numberPointer(1)}}}},
		{name: "numeric enum", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Enum: []string{"1"}}}}},
		{name: "non finite minimum", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat, Min: numberPointer(math.NaN())}}}},
		{name: "reversed range", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Min: numberPointer(2), Max: numberPointer(1)}}}},
		{name: "integer range without integer", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Min: numberPointer(1.1), Max: numberPointer(1.9)}}}},
		{name: "integer range above int64", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Min: numberPointer(math.Exp2(63)), Max: numberPointer(math.Exp2(63))}}}},
		{name: "integer range below int64", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Min: numberPointer(-math.MaxFloat64), Max: numberPointer(-math.Exp2(64))}}}},
		{name: "empty enum", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeEnum}}}},
		{name: "duplicate enum", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeEnum, Enum: []string{"a", "a"}}}}},
		{name: "invalid integer default", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Default: "1.5"}}}},
		{name: "default outside range", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat, Default: "2", Max: numberPointer(1)}}}},
		{name: "invalid enum default", spec: CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeEnum, Default: "other", Enum: []string{"known"}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireCategory(t, ValidateSpec(test.spec), sdk.CategoryInvalidArgument)
		})
	}
}

// TestValidateSpecAcceptsIntegerRangeWithBoundaryValue 验证整数闭区间只要包含 int64 就保持可用
func TestValidateSpecAcceptsIntegerRangeWithBoundaryValue(t *testing.T) {
	spec := CommandSpec{Path: []string{"example"}, Args: []ArgSpec{{
		Name: "value", Type: ArgTypeInt, Default: "2", Min: numberPointer(1.1), Max: numberPointer(2),
	}}}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("integer range containing its upper boundary was rejected: %v", err)
	}
}

// TestValidateArgsCopiesAndCompletes 验证参数副本、默认值和可选布尔缺省语义
func TestValidateArgsCopiesAndCompletes(t *testing.T) {
	spec := CommandSpec{Path: []string{"example"}, Args: []ArgSpec{
		{Name: "text", Type: ArgTypeString, Required: true},
		{Name: "count", Type: ArgTypeInt, Default: "2", Min: numberPointer(1), Max: numberPointer(3)},
		{Name: "enabled", Type: ArgTypeBool},
		{Name: "note", Type: ArgTypeString},
	}}
	supplied := map[string]string{"text": ""}
	validated, err := ValidateArgs(spec, supplied)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"text": "", "count": "2", "enabled": "false"}
	if !reflect.DeepEqual(validated, want) {
		t.Fatalf("unexpected validated arguments: %#v", validated)
	}
	validated["text"] = "changed"
	if supplied["text"] != "" || len(supplied) != 1 {
		t.Fatal("validation mutated the supplied map")
	}
}

// TestValidateArgsRejectsInvalidInput 验证未知、缺失、溢出、非有限及越界输入
func TestValidateArgsRejectsInvalidInput(t *testing.T) {
	base := CommandSpec{Path: []string{"example"}}
	tests := []struct {
		name     string
		spec     CommandSpec
		supplied map[string]string
	}{
		{name: "unknown", spec: base, supplied: map[string]string{"other": "value"}},
		{name: "missing required despite default", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeString, Required: true, Default: "fallback"}}}},
		{name: "empty integer", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt}}}, supplied: map[string]string{"value": ""}},
		{name: "integer overflow", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt}}}, supplied: map[string]string{"value": "9223372036854775808"}},
		{name: "integer outside range", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Min: numberPointer(1), Max: numberPointer(2)}}}, supplied: map[string]string{"value": "3"}},
		{name: "float overflow", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat}}}, supplied: map[string]string{"value": "1e10000"}},
		{name: "nan", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat}}}, supplied: map[string]string{"value": "NaN"}},
		{name: "infinity", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat}}}, supplied: map[string]string{"value": "+Inf"}},
		{name: "float outside range", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeFloat, Max: numberPointer(1)}}}, supplied: map[string]string{"value": "1.1"}},
		{name: "invalid bool", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeBool}}}, supplied: map[string]string{"value": "yes"}},
		{name: "invalid enum", spec: CommandSpec{Path: base.Path, Args: []ArgSpec{{Name: "value", Type: ArgTypeEnum, Enum: []string{"a", "b"}}}}, supplied: map[string]string{"value": "c"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateArgs(test.spec, test.supplied)
			requireCategory(t, err, sdk.CategoryInvalidArgument)
		})
	}
}

// TestValidateArgsPreservesEmptyDefaultContract 验证空 Default 没有存在位且枚举仍可显式接收空值
func TestValidateArgsPreservesEmptyDefaultContract(t *testing.T) {
	spec := CommandSpec{Path: []string{"example"}, Args: []ArgSpec{
		{Name: "text", Type: ArgTypeString, Default: ""},
		{Name: "mode", Type: ArgTypeEnum, Enum: []string{"", "safe"}},
	}}
	validated, err := ValidateArgs(spec, map[string]string{"mode": ""})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := validated["text"]; exists || validated["mode"] != "" {
		t.Fatalf("empty default or explicit empty value lost its contract: %#v", validated)
	}
}
