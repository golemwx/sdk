package plugin

import "reflect"

func deepCopy[T any](value T) T {
	copied := deepCopyValue(reflect.ValueOf(value))
	if !copied.IsValid() {
		var zero T
		return zero
	}
	return copied.Interface().(T)
}

// deepCopyValue 复制 map、slice、指针和接口，避免候选值与当前快照共享引用。
//
// 结构体先整体赋值，再替换可设置的字段。不可设置的字段（例如 time.Time
// 内部状态）保持浅拷贝；配置类型不应把可变状态放在未导出字段中。
func deepCopyValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.New(value.Type().Elem())
		copied.Elem().Set(deepCopyValue(value.Elem()))
		return copied
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.New(value.Type()).Elem()
		copied.Set(deepCopyValue(value.Elem()))
		return copied
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := range value.Len() {
			copied.Index(index).Set(deepCopyValue(value.Index(index)))
		}
		return copied
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			copied.SetMapIndex(deepCopyValue(iterator.Key()), deepCopyValue(iterator.Value()))
		}
		return copied
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		for index := range value.Len() {
			copied.Index(index).Set(deepCopyValue(value.Index(index)))
		}
		return copied
	case reflect.Struct:
		copied := reflect.New(value.Type()).Elem()
		copied.Set(value)
		for index := range value.NumField() {
			field := copied.Field(index)
			source := value.Field(index)
			if !field.CanSet() || !source.CanInterface() {
				continue
			}
			field.Set(deepCopyValue(source))
		}
		return copied
	default:
		return value
	}
}
