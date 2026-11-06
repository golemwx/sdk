package plugin

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/golemwx/sdk"
	toml "github.com/pelletier/go-toml/v2"
)

var (
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	timeType          = reflect.TypeOf(time.Time{})
)

func decodeConfig[T any](data []byte) (T, error) {
	var value T
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		var zero T
		return zero, translateTOMLError(err)
	}
	return value, nil
}

func translateTOMLError(err error) error {
	var missing *toml.StrictMissingError
	if errors.As(err, &missing) {
		keys := make([]string, 0, len(missing.Errors))
		for _, field := range missing.Errors {
			key := strings.Join(field.Key(), ".")
			if key == "" {
				key = "?"
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.decode", "配置包含未知字段："+strings.Join(keys, "、"))
	}
	var decode *toml.DecodeError
	if errors.As(err, &decode) {
		if key := strings.Join(decode.Key(), "."); key != "" {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.decode", fmt.Sprintf("配置字段 %q 格式无效", key))
		}
	}
	return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.decode", "配置格式无效")
}

func encodeConfig[T any](value T) ([]byte, error) {
	normalized, nilPointers := normalizeForTOML(reflect.ValueOf(value), "", true)
	if !normalized.IsValid() {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.encode", "配置类型不能是空值")
	}
	data, err := toml.Marshal(normalized.Interface())
	if err != nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.config.encode", "配置无法编码为 TOML")
	}
	if len(nilPointers) == 0 {
		return data, nil
	}
	var output bytes.Buffer
	output.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		output.WriteByte('\n')
	}
	for _, pointer := range nilPointers {
		output.WriteString("\n# 字段 ")
		output.WriteString(pointer.path)
		output.WriteString(" 未设置（nil）时保持为空；示例：\n")
		writeCommentExample(&output, pointer.path, pointer.typ)
	}
	return output.Bytes(), nil
}

// configEquivalent 比较配置语义，把 TOML 无法表达的 nil 集合与空集合视为相同。
func configEquivalent[T any](left, right T) bool {
	normalizedLeft, _ := normalizeForTOML(reflect.ValueOf(left), "", false)
	normalizedRight, _ := normalizeForTOML(reflect.ValueOf(right), "", false)
	if !normalizedLeft.IsValid() || !normalizedRight.IsValid() {
		return normalizedLeft.IsValid() == normalizedRight.IsValid()
	}
	return reflect.DeepEqual(normalizedLeft.Interface(), normalizedRight.Interface())
}

type nilPointer struct {
	path string
	typ  reflect.Type
}

func normalizeForTOML(value reflect.Value, path string, track bool) (reflect.Value, []nilPointer) {
	if !value.IsValid() {
		return value, nil
	}
	if implementsTOMLMarshaler(value.Type()) {
		return value, nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			if track && path != "" {
				return reflect.Zero(value.Type()), []nilPointer{{path: path, typ: value.Type().Elem()}}
			}
			return reflect.Zero(value.Type()), nil
		}
		normalized, pointers := normalizeForTOML(value.Elem(), path, track)
		copied := reflect.New(value.Type().Elem())
		copied.Elem().Set(normalized)
		return copied, pointers
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), nil
		}
		normalized, pointers := normalizeForTOML(value.Elem(), path, track)
		copied := reflect.New(value.Type()).Elem()
		copied.Set(normalized)
		return copied, pointers
	case reflect.Slice:
		if value.IsNil() {
			return reflect.MakeSlice(value.Type(), 0, 0), nil
		}
		copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		var pointers []nilPointer
		for index := range value.Len() {
			normalized, nested := normalizeForTOML(value.Index(index), "", false)
			copied.Index(index).Set(normalized)
			pointers = append(pointers, nested...)
		}
		return copied, pointers
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		var pointers []nilPointer
		for index := range value.Len() {
			normalized, nested := normalizeForTOML(value.Index(index), "", false)
			copied.Index(index).Set(normalized)
			pointers = append(pointers, nested...)
		}
		return copied, pointers
	case reflect.Map:
		if value.IsNil() {
			return reflect.MakeMap(value.Type()), nil
		}
		copied := reflect.MakeMapWithSize(value.Type(), value.Len())
		var pointers []nilPointer
		iterator := value.MapRange()
		for iterator.Next() {
			key, keyPointers := normalizeForTOML(iterator.Key(), "", false)
			item, valuePointers := normalizeForTOML(iterator.Value(), "", false)
			copied.SetMapIndex(key, item)
			pointers = append(pointers, keyPointers...)
			pointers = append(pointers, valuePointers...)
		}
		return copied, pointers
	case reflect.Struct:
		copied := reflect.New(value.Type()).Elem()
		copied.Set(value)
		var pointers []nilPointer
		for index := range value.NumField() {
			field := copied.Field(index)
			source := value.Field(index)
			if !field.CanSet() || !source.CanInterface() {
				continue
			}
			name := tomlFieldName(value.Type().Field(index))
			if name == "" {
				continue
			}
			normalized, nested := normalizeForTOML(source, joinTOMLPath(path, name), track)
			field.Set(normalized)
			pointers = append(pointers, nested...)
		}
		return copied, pointers
	default:
		return value, nil
	}
}

func implementsTOMLMarshaler(typ reflect.Type) bool {
	if typ == timeType {
		return true
	}
	return typ.Implements(textMarshalerType)
}

func joinTOMLPath(parent, child string) string {
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return parent + "." + child
}

func tomlFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("toml")
	if tag == "-" {
		return ""
	}
	if tag != "" {
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			return name
		}
	}
	return lowerFirstRune(field.Name)
}

func lowerFirstRune(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func writeCommentExample(output *bytes.Buffer, path string, typ reflect.Type) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == timeType {
		fmt.Fprintf(output, "# %s = 1970-01-01T00:00:00Z\n", path)
		return
	}
	if typ.Implements(textMarshalerType) {
		zero := reflect.Zero(typ).Interface().(encoding.TextMarshaler)
		text, err := zero.MarshalText()
		if err == nil {
			fmt.Fprintf(output, "# %s = %q\n", path, string(text))
			return
		}
	}
	switch typ.Kind() {
	case reflect.Struct:
		written := false
		for index := range typ.NumField() {
			field := typ.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := tomlFieldName(field)
			if name == "" {
				continue
			}
			writeCommentExample(output, joinTOMLPath(path, name), field.Type)
			written = true
		}
		if !written {
			fmt.Fprintf(output, "# %s = {}\n", path)
		}
	case reflect.Map:
		fmt.Fprintf(output, "# %s = {}\n", path)
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(output, "# %s = []\n", path)
	case reflect.String:
		fmt.Fprintf(output, "# %s = ''\n", path)
	case reflect.Bool:
		fmt.Fprintf(output, "# %s = false\n", path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		fmt.Fprintf(output, "# %s = 0\n", path)
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(output, "# %s = 0.0\n", path)
	default:
		fmt.Fprintf(output, "# %s = ''\n", path)
	}
}
