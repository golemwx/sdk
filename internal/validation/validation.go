// Package validation 统一封装插件配置和命令共用的 validator/v10 规则。
package validation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/go-playground/validator/v10"
	"github.com/golemwx/sdk"
	"github.com/robfig/cron/v3"
)

const (
	// TagDuration 使用 time.ParseDuration 校验时长文本。
	TagDuration = "duration"
	// TagCron 使用 robfig/cron Parser 校验五字段 Cron 表达式或 descriptor。
	TagCron = "cron"
)

var (
	sharedValidator = newValidator()
	cronParser      = cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	durationType = reflect.TypeOf(time.Duration(0))
)

// newValidator 在包加载时注册通用规则，进程内共享同一个只读校验器。
func newValidator() *validator.Validate {
	value := validator.New(validator.WithRequiredStructEnabled())
	if err := value.RegisterValidation(TagDuration, validateDuration); err != nil {
		panic("register duration validation: " + err.Error())
	}
	if err := value.RegisterValidation(TagCron, validateCron); err != nil {
		panic("register cron validation: " + err.Error())
	}
	return value
}

// Struct 校验结构体，并把字段错误转换为中文字段消息。
func Struct(value any, operation string) error {
	if value == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "校验目标不能为空")
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer && reflected.IsNil() {
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, "校验目标不能是空指针")
	}
	root := reflected.Type()
	for root.Kind() == reflect.Pointer {
		root = root.Elem()
	}
	if err := sharedValidator.Struct(value); err != nil {
		return translate(err, operation, root)
	}
	return nil
}

func translate(err error, operation string, root reflect.Type) error {
	var business *sdk.Error
	if errors.As(err, &business) {
		return business
	}
	var invalid *validator.InvalidValidationError
	if errors.As(err, &invalid) {
		return sdk.NewError(sdk.CategoryInternal, operation, "校验目标不是结构体")
	}
	var fields validator.ValidationErrors
	if errors.As(err, &fields) {
		messages := make([]string, 0, len(fields))
		for _, field := range fields {
			messages = append(messages, fieldMessage(field, root))
		}
		return sdk.NewError(sdk.CategoryInvalidArgument, operation, strings.Join(messages, "；"))
	}
	return sdk.NewError(sdk.CategoryInvalidArgument, operation, "参数校验失败")
}

func fieldMessage(field validator.FieldError, root reflect.Type) string {
	return fmt.Sprintf("字段 %q 无效：%s", fieldName(field.StructNamespace(), root), fieldReason(field))
}

func fieldName(namespace string, root reflect.Type) string {
	if namespace == "" {
		return "参数"
	}
	segments := strings.Split(namespace, ".")
	current := root
	for current.Kind() == reflect.Pointer {
		current = current.Elem()
	}
	name := ""
	for _, segment := range segments {
		if current.Kind() == reflect.Struct && current.Name() == segment {
			continue
		}
		base, suffix := splitIndex(segment)
		if current.Kind() != reflect.Struct {
			name += "." + strings.ToLower(base) + suffix
			continue
		}
		field, ok := current.FieldByName(base)
		if !ok {
			name += "." + lowerFirst(base) + suffix
			continue
		}
		part := publicFieldName(field)
		if name == "" {
			name = part + suffix
		} else {
			name += "." + part + suffix
		}
		current = indirectType(field.Type)
	}
	if name == "" {
		return "参数"
	}
	return name
}

func splitIndex(segment string) (string, string) {
	if index := strings.IndexByte(segment, '['); index >= 0 {
		return segment[:index], segment[index:]
	}
	return segment, ""
}

func publicFieldName(field reflect.StructField) string {
	for _, tag := range []string{"toml", "name", "json"} {
		value := field.Tag.Get(tag)
		if value == "" || value == "-" {
			continue
		}
		if name, _, _ := strings.Cut(value, ","); name != "" {
			return name
		}
	}
	return lowerFirst(field.Name)
}

func lowerFirst(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func indirectType(value reflect.Type) reflect.Type {
	for {
		switch value.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			value = value.Elem()
		default:
			return value
		}
	}
}

func fieldReason(field validator.FieldError) string {
	param := field.Param()
	switch field.Tag() {
	case "required":
		return "不能为空"
	case "min":
		if isSizedKind(field.Kind()) {
			return "长度不能小于 " + param
		}
		return "不能小于 " + param
	case "max":
		if isSizedKind(field.Kind()) {
			return "长度不能大于 " + param
		}
		return "不能大于 " + param
	case "len":
		return "长度必须等于 " + param
	case "oneof":
		return "必须是允许的值之一"
	case "url":
		return "不是有效的 URL"
	case TagDuration:
		return "不是有效的时长"
	case TagCron:
		return "不是有效的 Cron 表达式"
	case "gtfield":
		return "必须大于字段 " + param
	case "gtefield":
		return "必须大于或等于字段 " + param
	case "ltfield":
		return "必须小于字段 " + param
	case "ltefield":
		return "必须小于或等于字段 " + param
	case "eqfield":
		return "必须等于字段 " + param
	case "nefield":
		return "不能等于字段 " + param
	default:
		return "不满足 " + field.Tag() + " 约束"
	}
}

func isSizedKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}

func validateDuration(field validator.FieldLevel) bool {
	value := indirectValue(field.Field())
	if !value.IsValid() {
		return false
	}
	if value.Type() == durationType {
		return true
	}
	if value.Kind() != reflect.String {
		return false
	}
	_, err := time.ParseDuration(value.String())
	return err == nil
}

func validateCron(field validator.FieldLevel) bool {
	value := indirectValue(field.Field())
	if !value.IsValid() || value.Kind() != reflect.String {
		return false
	}
	_, err := cronParser.Parse(value.String())
	return err == nil
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}
