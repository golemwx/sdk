package plugin

import (
	"context"
	"reflect"

	"github.com/golemwx/sdk"
)

// configHandle 是 Runtime 与泛型 Config[T] 之间的非泛型桥接。
type configHandle interface {
	ConfigReceiver
	defaultTOML() ([]byte, error)
	validateTOML(ctx context.Context, data []byte) error
	loadInitial(ctx context.Context, data []byte) error
	configStatus() Status
	setOwner(owner Plugin)
	setClient(client ConfigClient)
	setStatusListener(listener func(Status))
}

var configHandleType = reflect.TypeOf((*configHandle)(nil)).Elem()

// discoverConfig 查找插件实例匿名嵌入的 Config[T]。
//
// 只扫描匿名嵌入字段，不扫描插件内部的其他配置结构，也不使用 unsafe。
func discoverConfig(implementation Plugin) (configHandle, error) {
	if implementation == nil {
		return nil, nil
	}
	value := reflect.ValueOf(implementation)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "插件实现必须是非空指针")
	}

	var found configHandle
	visited := make(map[reflect.Type]bool)
	var walk func(reflect.Value) error
	walk = func(current reflect.Value) error {
		if !current.IsValid() {
			return nil
		}
		if current.Kind() == reflect.Pointer {
			if current.IsNil() {
				if current.Type().Implements(configHandleType) {
					return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "嵌入的配置句柄未初始化")
				}
				return nil
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Struct {
			return nil
		}
		typ := current.Type()
		if visited[typ] {
			return nil
		}
		visited[typ] = true
		for index := range current.NumField() {
			field := typ.Field(index)
			if !field.Anonymous {
				continue
			}
			fieldValue := current.Field(index)
			if handle, ok := configFromValue(fieldValue); ok {
				if found != nil {
					return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "插件只能嵌入一个配置句柄")
				}
				found = handle
				continue
			}
			if err := walk(fieldValue); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(value); err != nil {
		return nil, err
	}
	return found, nil
}

func configFromValue(value reflect.Value) (configHandle, bool) {
	if !value.IsValid() {
		return nil, false
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return nil, false
	}
	if value.Type().Implements(configHandleType) && value.CanInterface() {
		handle, ok := value.Interface().(configHandle)
		return handle, ok
	}
	if value.Kind() != reflect.Pointer && value.CanAddr() {
		pointer := value.Addr()
		if pointer.Type().Implements(configHandleType) && pointer.CanInterface() {
			handle, ok := pointer.Interface().(configHandle)
			return handle, ok
		}
	}
	return nil, false
}
