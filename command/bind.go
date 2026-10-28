package command

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/golemwx/sdk"
)

const bindTag = "command" // 导出字段与命名参数之间的稳定绑定标签

// Bind 将已完成 schema 校验的命名参数绑定到目标结构体的导出字段
// 目标字段必须使用 command 标签显式对应参数名，未声明的参数不能被静默忽略
func Bind(spec CommandSpec, supplied map[string]string, target any) error {
	if err := ValidateSpec(spec); err != nil {
		return err
	}
	values, err := ValidateArgs(spec, supplied)
	if err != nil {
		return bindValidationError(spec, supplied, err)
	}
	if target == nil {
		return bindingError("", "", "目标必须是可写结构体的非空指针")
	}
	pointer := reflect.ValueOf(target)
	if pointer.Kind() != reflect.Pointer || pointer.IsNil() {
		return bindingError("", "", "目标必须是可写结构体的非空指针")
	}
	value := pointer.Elem()
	if value.Kind() != reflect.Struct {
		return bindingError("", "", "目标指针必须指向结构体")
	}

	args := make(map[string]ArgSpec, len(spec.Args))
	for _, arg := range spec.Args {
		args[arg.Name] = arg
	}
	bound := make(map[string]bool, len(spec.Args))
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		meta := valueType.Field(index)
		name, tagged := meta.Tag.Lookup(bindTag)
		if !tagged || name == "-" {
			continue
		}
		if meta.PkgPath != "" {
			return bindingError(name, meta.Name, "只有导出字段可以使用 command 标签")
		}
		if !field.CanSet() {
			return bindingError(name, meta.Name, "目标字段不可写，请检查结构体定义")
		}
		if name == "" {
			return bindingError("", meta.Name, "command 标签必须填写参数名")
		}
		arg, exists := args[name]
		if !exists {
			return bindingError(name, meta.Name, "目标字段引用了未声明的参数，请修改 command 标签或删除该字段")
		}
		if bound[name] {
			return bindingError(name, meta.Name, "多个目标字段不能绑定同一参数")
		}
		if err := checkFieldType(arg, field); err != nil {
			return bindingError(name, meta.Name, err.Error())
		}
		text, present := values[name]
		if err := bindField(arg, field, text, present); err != nil {
			return bindingError(name, meta.Name, err.Error())
		}
		bound[name] = true
	}
	for _, arg := range spec.Args {
		if !bound[arg.Name] {
			return bindingError(arg.Name, "", "缺少对应的导出字段，请添加带 command 标签的可写字段")
		}
	}
	return nil
}

// bindValidationError 为输入校验失败补充参数名和修复方向，声明错误仍保留原类别
func bindValidationError(spec CommandSpec, supplied map[string]string, fallback error) error {
	declared := make(map[string]ArgSpec, len(spec.Args))
	for _, arg := range spec.Args {
		declared[arg.Name] = arg
	}
	names := make([]string, 0, len(supplied))
	for name := range supplied {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		arg, exists := declared[name]
		if !exists {
			return bindingError(name, "", "参数未在 CommandSpec 中声明，请检查命令声明或输入")
		}
		if !validArgValue(arg, supplied[name]) {
			return bindingError(name, "", "参数值无效，请检查类型、范围和枚举约束")
		}
	}
	for _, arg := range spec.Args {
		if _, exists := supplied[arg.Name]; arg.Required && !exists {
			return bindingError(arg.Name, "", "必填参数缺失，请提供该参数")
		}
	}
	return fallback
}

// checkFieldType 在解析值之前确认声明类型与目标字段兼容
func checkFieldType(arg ArgSpec, field reflect.Value) error {
	kind := field.Kind()
	if kind == reflect.Pointer {
		element := field.Type().Elem()
		if element.Kind() == reflect.Pointer {
			return fmt.Errorf("目标字段只允许一层指针，请改为对应基础类型的指针")
		}
		kind = element.Kind()
	}
	switch arg.Type {
	case ArgTypeString, ArgTypeEnum:
		if kind != reflect.String {
			return fmt.Errorf("参数类型为文本，目标字段必须是 string 或 *string，请修改字段类型")
		}
	case ArgTypeInt:
		if !isIntKind(kind) {
			return fmt.Errorf("参数类型为整数，目标字段必须是整数类型或其指针，请修改字段类型")
		}
	case ArgTypeFloat:
		if kind != reflect.Float32 && kind != reflect.Float64 {
			return fmt.Errorf("参数类型为数值，目标字段必须是 float32、float64 或其指针")
		}
	case ArgTypeBool:
		if kind != reflect.Bool {
			return fmt.Errorf("参数类型为布尔开关，目标字段必须是 bool 或 *bool")
		}
	default:
		return fmt.Errorf("参数类型未知，请先修正 CommandSpec")
	}
	return nil
}

// bindField 将缺失值写为类型零值，并用指针字段保留可选参数是否携带
func bindField(arg ArgSpec, field reflect.Value, text string, present bool) error {
	if field.Kind() != reflect.Pointer {
		field.SetZero()
		if present {
			return setField(arg, field, text)
		}
		return nil
	}
	if !present {
		field.SetZero()
		return nil
	}
	element := reflect.New(field.Type().Elem())
	if err := setField(arg, element.Elem(), text); err != nil {
		return err
	}
	field.Set(element)
	return nil
}

// setField 使用 schema 已校验的值写入具体导出字段
func setField(arg ArgSpec, field reflect.Value, text string) error {
	switch arg.Type {
	case ArgTypeString, ArgTypeEnum:
		field.SetString(text)
	case ArgTypeInt:
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("参数值无法解析为整数，请检查命令输入")
		}
		if isSignedIntKind(field.Kind()) {
			if field.OverflowInt(number) {
				return fmt.Errorf("参数值超出目标整数类型的范围，请修改命令输入或字段类型")
			}
			field.SetInt(number)
			return nil
		}
		if number < 0 || field.OverflowUint(uint64(number)) {
			return fmt.Errorf("参数值超出目标无符号整数类型的范围，请修改命令输入或字段类型")
		}
		field.SetUint(uint64(number))
	case ArgTypeFloat:
		bitSize := 64
		if field.Kind() == reflect.Float32 {
			bitSize = 32
		}
		number, err := strconv.ParseFloat(text, bitSize)
		if err != nil || field.OverflowFloat(number) {
			return fmt.Errorf("参数值超出目标浮点类型范围，请修改命令输入或字段类型")
		}
		field.SetFloat(number)
	case ArgTypeBool:
		flag, err := strconv.ParseBool(text)
		if err != nil {
			return fmt.Errorf("参数值无法解析为布尔开关，请使用 true 或 false")
		}
		field.SetBool(flag)
	default:
		return fmt.Errorf("参数类型未知，请先修正 CommandSpec")
	}
	return nil
}

// isIntKind 判断反射字段是否属于整数类型
func isIntKind(kind reflect.Kind) bool {
	return isSignedIntKind(kind) || kind == reflect.Uint || kind == reflect.Uint8 ||
		kind == reflect.Uint16 || kind == reflect.Uint32 || kind == reflect.Uint64
}

// isSignedIntKind 判断反射字段是否属于有符号整数类型
func isSignedIntKind(kind reflect.Kind) bool {
	return kind == reflect.Int || kind == reflect.Int8 || kind == reflect.Int16 ||
		kind == reflect.Int32 || kind == reflect.Int64
}

// bindingError 创建包含参数名和修复方向的稳定绑定错误
func bindingError(argument, field, reason string) error {
	target := "目标结构体"
	if field != "" {
		target = "字段 " + field
	}
	if argument != "" {
		target = "参数 " + argument + " 的" + target
	}
	return sdk.NewError(sdk.CategoryInvalidArgument, "command.bind", target+"："+reason)
}
