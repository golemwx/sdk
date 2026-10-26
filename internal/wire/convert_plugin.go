package wire

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/contract"
)

// toSubscribeMode 编码订阅模式，未知值归一为零值
func toSubscribeMode(value event.SubscribeMode) SubscribeMode {
	if value < event.SubscribeModeUnknown || value > event.SubscribeModeObserve {
		return SubscribeMode_SUBSCRIBE_MODE_UNKNOWN
	}

	return SubscribeMode(value)
}

// fromSubscribeMode 解码订阅模式，未知值归一为零值
func fromSubscribeMode(value SubscribeMode) event.SubscribeMode {
	if value < SubscribeMode_SUBSCRIBE_MODE_UNKNOWN || value > SubscribeMode_SUBSCRIBE_MODE_OBSERVE {
		return event.SubscribeModeUnknown
	}

	return event.SubscribeMode(value)
}

// toArgType 编码参数类型，未知值归一为零值
func toArgType(value command.ArgType) ArgType {
	if value < command.ArgTypeUnknown || value > command.ArgTypeEnum {
		return ArgType_ARG_TYPE_UNKNOWN
	}

	return ArgType(value)
}

// fromArgType 解码参数类型，未知值归一为零值
func fromArgType(value ArgType) command.ArgType {
	if value < ArgType_ARG_TYPE_UNKNOWN || value > ArgType_ARG_TYPE_ENUM {
		return command.ArgTypeUnknown
	}

	return command.ArgType(value)
}

// toWireSubscription 编码事件订阅声明
func toWireSubscription(value event.Subscription) *Subscription {
	return &Subscription{Topic: value.Topic, Mode: toSubscribeMode(value.Mode), Priority: int32(value.Priority)}
}

// fromWireSubscription 解码事件订阅声明
func fromWireSubscription(value *Subscription) event.Subscription {
	if value == nil {
		return event.Subscription{}
	}

	return event.Subscription{Topic: value.Topic, Mode: fromSubscribeMode(value.Mode), Priority: int(value.Priority)}
}

// toWireArgSpec 编码命令参数声明并复制可选边界
func toWireArgSpec(value command.ArgSpec) *ArgSpec {
	return &ArgSpec{
		Name:       value.Name,
		Type:       toArgType(value.Type),
		Required:   value.Required,
		Default:    value.Default,
		EnumValues: append([]string(nil), value.Enum...),
		Min:        cloneFloat64(value.Min),
		Max:        cloneFloat64(value.Max),
		Help:       value.Help,
	}
}

// fromWireArgSpec 解码命令参数声明并复制可选边界
func fromWireArgSpec(value *ArgSpec) command.ArgSpec {
	if value == nil {
		return command.ArgSpec{}
	}

	return command.ArgSpec{
		Name:     value.Name,
		Type:     fromArgType(value.Type),
		Required: value.Required,
		Default:  value.Default,
		Enum:     append([]string{}, value.EnumValues...),
		Min:      cloneFloat64(value.Min),
		Max:      cloneFloat64(value.Max),
		Help:     value.Help,
	}
}

// toWireCommandSpec 编码命令及参数声明
func toWireCommandSpec(value command.CommandSpec) *CommandSpec {
	args := make([]*ArgSpec, 0, len(value.Args))
	for _, arg := range value.Args {
		args = append(args, toWireArgSpec(arg))
	}

	return &CommandSpec{
		Path:       append([]string(nil), value.Path...),
		Args:       args,
		Permission: string(value.Permission),
		Help:       value.Help,
	}
}

// fromWireCommandSpec 解码命令及参数声明
func fromWireCommandSpec(value *CommandSpec) command.CommandSpec {
	if value == nil {
		return command.CommandSpec{}
	}

	args := make([]command.ArgSpec, 0, len(value.Args))
	for _, arg := range value.Args {
		args = append(args, fromWireArgSpec(arg))
	}

	return command.CommandSpec{
		Path:       append([]string{}, value.Path...),
		Args:       args,
		Permission: sdk.Permission(value.Permission),
		Help:       value.Help,
	}
}

// toWireCapabilitySpec 编码公开能力声明
func toWireCapabilitySpec(value capability.CapabilitySpec) *CapabilitySpec {
	return &CapabilitySpec{
		Name:          value.Name,
		InputType:     value.InputType,
		OutputType:    value.OutputType,
		Description:   value.Description,
		MaxInlineSize: int32(value.MaxInlineSize),
	}
}

// fromWireCapabilitySpec 解码公开能力声明
func fromWireCapabilitySpec(value *CapabilitySpec) capability.CapabilitySpec {
	if value == nil {
		return capability.CapabilitySpec{}
	}

	return capability.CapabilitySpec{
		Name:          value.Name,
		InputType:     value.InputType,
		OutputType:    value.OutputType,
		Description:   value.Description,
		MaxInlineSize: int(value.MaxInlineSize),
	}
}

// ToWireDescriptor 转换插件描述
func ToWireDescriptor(value contract.Descriptor) *Descriptor {
	permissions := make([]string, 0, len(value.RequestedPermissions))
	for _, permission := range value.RequestedPermissions {
		permissions = append(permissions, string(permission))
	}

	subscriptions := make([]*Subscription, 0, len(value.Subscriptions))
	for _, subscription := range value.Subscriptions {
		subscriptions = append(subscriptions, toWireSubscription(subscription))
	}

	commands := make([]*CommandSpec, 0, len(value.Commands))
	for _, commandSpec := range value.Commands {
		commands = append(commands, toWireCommandSpec(commandSpec))
	}

	capabilities := make([]*CapabilitySpec, 0, len(value.Capabilities))
	for _, capabilitySpec := range value.Capabilities {
		capabilities = append(capabilities, toWireCapabilitySpec(capabilitySpec))
	}

	return &Descriptor{
		Id:                   value.ID,
		Name:                 value.Name,
		Description:          value.Description,
		Author:               value.Author,
		Version:              value.Version,
		RequestedPermissions: permissions,
		Subscriptions:        subscriptions,
		Commands:             commands,
		Capabilities:         capabilities,
		UsesCapabilities:     append([]string(nil), value.UsesCapabilities...),
	}
}

// FromWireDescriptor 转换 wire 插件描述
func FromWireDescriptor(value *Descriptor) contract.Descriptor {
	if value == nil {
		return contract.Descriptor{}
	}

	permissions := make([]sdk.Permission, 0, len(value.RequestedPermissions))
	for _, permission := range value.RequestedPermissions {
		permissions = append(permissions, sdk.Permission(permission))
	}

	subscriptions := make([]event.Subscription, 0, len(value.Subscriptions))
	for _, subscription := range value.Subscriptions {
		subscriptions = append(subscriptions, fromWireSubscription(subscription))
	}

	commands := make([]command.CommandSpec, 0, len(value.Commands))
	for _, commandSpec := range value.Commands {
		commands = append(commands, fromWireCommandSpec(commandSpec))
	}

	capabilities := make([]capability.CapabilitySpec, 0, len(value.Capabilities))
	for _, capabilitySpec := range value.Capabilities {
		capabilities = append(capabilities, fromWireCapabilitySpec(capabilitySpec))
	}

	return contract.Descriptor{
		ID:                   value.Id,
		Name:                 value.Name,
		Description:          value.Description,
		Author:               value.Author,
		Version:              value.Version,
		RequestedPermissions: permissions,
		Subscriptions:        subscriptions,
		Commands:             commands,
		Capabilities:         capabilities,
		UsesCapabilities:     append([]string{}, value.UsesCapabilities...),
	}
}
