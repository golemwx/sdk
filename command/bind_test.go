package command

import (
	"errors"
	"strings"
	"testing"

	"github.com/golemwx/sdk"
)

// typedArguments 覆盖全部首期参数类型和默认值
type typedArguments struct {
	Name    string  `command:"name"`    // 文本参数
	Count   int     `command:"count"`   // 整数参数
	Ratio   float64 `command:"ratio"`   // 浮点参数
	Enabled bool    `command:"enabled"` // 布尔参数
	Color   string  `command:"color"`   // 枚举参数
	Ignored string  `command:"-"`       // 显式排除的本地字段
}

// bindSpec 建立与 typedArguments 一致的可执行命令声明
func bindSpec() CommandSpec {
	return CommandSpec{Path: []string{"typed", "run"}, Args: []ArgSpec{
		{Name: "name", Type: ArgTypeString, Required: true},
		{Name: "count", Type: ArgTypeInt, Default: "2"},
		{Name: "ratio", Type: ArgTypeFloat},
		{Name: "enabled", Type: ArgTypeBool},
		{Name: "color", Type: ArgTypeEnum, Enum: []string{"red", "blue"}, Default: "red"},
	}}
}

// TestBindArguments 验证导出字段、默认值和全部基础类型
func TestBindArguments(t *testing.T) {
	target := typedArguments{Ignored: "retained"}
	if err := Bind(bindSpec(), map[string]string{"name": "demo", "ratio": "1.5", "enabled": "true"}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Name != "demo" || target.Count != 2 || target.Ratio != 1.5 || !target.Enabled || target.Color != "red" {
		t.Fatalf("绑定结果错误：%+v", target)
	}
	if target.Ignored != "retained" {
		t.Fatalf("command:\"-\" 字段被意外修改：%+v", target)
	}
	if err := Bind(bindSpec(), map[string]string{"name": "again"}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Ratio != 0 || target.Enabled {
		t.Fatalf("可选参数缺失时未清零旧值：%+v", target)
	}
}

// TestBindInvalidSpecIdentifiesArgument 验证 schema 错误也包含具体参数名
func TestBindInvalidSpecIdentifiesArgument(t *testing.T) {
	specs := []CommandSpec{
		{Path: []string{"typed"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeString}, {Name: "value", Type: ArgTypeString}}},
		{Path: []string{"typed"}, Args: []ArgSpec{{Name: "count", Type: ArgTypeInt, Default: "not-an-int"}}},
		{Path: []string{"typed"}, Args: []ArgSpec{{Name: "mode", Type: ArgType(99)}}},
	}
	for _, spec := range specs {
		err := Bind(spec, nil, &struct{}{})
		if !bindingCategory(err) || !strings.Contains(err.Error(), spec.Args[0].Name) {
			t.Fatalf("schema 错误缺少参数名：%v", err)
		}
	}
}

// TestBindOptionalPointers 验证指针字段区分缺失、显式空值和默认值
func TestBindOptionalPointers(t *testing.T) {
	type arguments struct {
		Name    *string  `command:"name"`
		Count   *int     `command:"count"`
		Ratio   *float64 `command:"ratio"`
		Enabled *bool    `command:"enabled"`
		Color   *string  `command:"color"`
	}
	var target arguments
	if err := Bind(bindSpec(), map[string]string{"name": ""}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Name == nil || *target.Name != "" {
		t.Fatalf("必填显式空文本未保留: %+v", target.Name)
	}
	if target.Count == nil || *target.Count != 2 || target.Color == nil || *target.Color != "red" {
		t.Fatalf("默认值未绑定到指针字段: %+v", target)
	}
	if target.Ratio != nil {
		t.Fatalf("缺少默认值的可选参数应为 nil: %+v", target.Ratio)
	}
	if target.Enabled == nil || *target.Enabled {
		t.Fatalf("可选布尔参数缺失时应补 false: %+v", target.Enabled)
	}
	if err := Bind(bindSpec(), map[string]string{"name": "again", "ratio": "1.25"}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Ratio == nil || *target.Ratio != 1.25 {
		t.Fatalf("可选数值未绑定: %+v", target.Ratio)
	}
	if err := Bind(bindSpec(), map[string]string{"name": "reset"}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Ratio != nil {
		t.Fatalf("重新绑定未清除旧的指针值: %+v", target.Ratio)
	}
}

// TestBindIntegerWidths 验证反射写入会检查具体整数宽度
func TestBindIntegerWidths(t *testing.T) {
	type arguments struct {
		Byte uint8 `command:"value"`
	}
	spec := CommandSpec{Path: []string{"typed"}, Args: []ArgSpec{{Name: "value", Type: ArgTypeInt, Required: true}}}
	var target arguments
	if err := Bind(spec, map[string]string{"value": "255"}, &target); err != nil || target.Byte != 255 {
		t.Fatalf("合法宽度绑定失败：%+v %v", target, err)
	}
	for _, value := range []string{"256", "-1"} {
		if err := Bind(spec, map[string]string{"value": value}, &target); !bindingCategory(err) {
			t.Fatalf("越界值 %s 未被拒绝：%v", value, err)
		}
	}
}

// TestBindValidationErrorsIdentifyArgument 验证输入错误不会只返回无上下文的通用校验文案
func TestBindValidationErrorsIdentifyArgument(t *testing.T) {
	cases := []struct {
		name     string
		supplied map[string]string
		argument string
	}{
		{name: "缺少必填", argument: "name"},
		{name: "未知参数", supplied: map[string]string{"name": "demo", "other": "x"}, argument: "other"},
		{name: "值类型错误", supplied: map[string]string{"name": "demo", "count": "not-an-int"}, argument: "count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var target typedArguments
			err := Bind(bindSpec(), tc.supplied, &target)
			if !bindingCategory(err) || !strings.Contains(err.Error(), tc.argument) {
				t.Fatalf("校验错误缺少参数名：%v", err)
			}
		})
	}
}

// TestBindRejectsInvalidTargets 验证不可写、无标签和标签漂移均给出可操作错误
func TestBindRejectsInvalidTargets(t *testing.T) {
	type missing struct {
		Other string
	}
	type mismatch struct {
		Count string `command:"count"`
		Name  string `command:"name"`
	}
	type hidden struct {
		name  string `command:"name"` // 未导出字段不能绑定
		Count int    `command:"count"`
	}
	type duplicate struct {
		Name  string `command:"name"`
		Alias string `command:"name"`
	}
	type unknown struct {
		Name  string `command:"name"`
		Count int    `command:"count"`
		Other string `command:"other"`
	}
	type nestedPointer struct {
		Name  string    `command:"name"`
		Count int       `command:"count"`
		Ratio **float64 `command:"ratio"`
	}
	cases := []struct {
		name    string // 场景名
		target  any    // 绑定目标
		mention string // 必须出现在错误中的参数名或字段名
	}{
		{name: "空目标", target: nil, mention: "目标"},
		{name: "非指针", target: typedArguments{}, mention: "指针"},
		{name: "缺少字段", target: &missing{}, mention: "name"},
		{name: "类型不匹配", target: &mismatch{}, mention: "count"},
		{name: "未导出字段", target: &hidden{}, mention: "name"},
		{name: "重复绑定", target: &duplicate{}, mention: "name"},
		{name: "未知标签", target: &unknown{}, mention: "other"},
		{name: "多层指针", target: &nestedPointer{}, mention: "ratio"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Bind(bindSpec(), map[string]string{"name": "demo", "ratio": "1"}, tc.target)
			if !bindingCategory(err) || !strings.Contains(err.Error(), tc.mention) {
				t.Fatalf("错误缺少类别或可操作上下文：%v", err)
			}
		})
	}
}

// bindingCategory 只检查公共错误类别，不依赖具体错误文字
func bindingCategory(err error) bool {
	var value *sdk.Error
	return errors.As(err, &value) && value.Category == sdk.CategoryInvalidArgument
}
