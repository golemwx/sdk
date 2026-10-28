package wire

import (
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/config"
	"maps"
)

// ToWireCommand 转换命令
func ToWireCommand(value *command.Command) (*Command, error) {
	if value == nil {
		return nil, nil
	}

	messageValue, err := ToWireMessage(value.Message)
	if err != nil {
		return nil, err
	}

	return &Command{
		Path:       append([]string(nil), value.Path...),
		Args:       maps.Clone(value.Args),
		Positional: append([]string(nil), value.Positional...),
		Actor:      ToWireActor(value.Actor),
		Message:    messageValue,
		Raw:        value.Raw,
	}, nil
}

// FromWireCommand 转换 wire 命令
func FromWireCommand(value *Command) (*command.Command, error) {
	if value == nil {
		return nil, nil
	}

	messageValue, err := FromWireMessage(value.Message)
	if err != nil {
		return nil, err
	}

	return &command.Command{
		Path:       append([]string{}, value.Path...),
		Args:       maps.Clone(value.Args),
		Positional: append([]string{}, value.Positional...),
		Actor:      FromWireActor(value.Actor),
		Message:    messageValue,
		Raw:        value.Raw,
	}, nil
}

// ToWireCommandResult 转换命令结果
func ToWireCommandResult(value command.CommandResult) (*CommandResult, error) {
	media, err := ToWireMediaRef(value.Media)
	if err != nil {
		return nil, err
	}

	return &CommandResult{Replied: value.Replied, Text: cloneString(value.Text), Media: media}, nil
}

// FromWireCommandResult 转换 wire 命令结果
func FromWireCommandResult(value *CommandResult) (command.CommandResult, error) {
	if value == nil {
		return command.CommandResult{}, nil
	}

	media, err := FromWireMediaRef(value.Media)
	if err != nil {
		return command.CommandResult{}, err
	}

	return command.CommandResult{Replied: value.Replied, Text: cloneString(value.Text), Media: media}, nil
}

// ToWireCallRequest 转换能力请求
func ToWireCallRequest(value capability.CallRequest) *CallRequest {
	return &CallRequest{
		CapabilityName: value.CapabilityName,
		ContentType:    value.ContentType,
		Payload:        toBytesValue(value.Payload),
	}
}

// FromWireCallRequest 转换 wire 能力请求
func FromWireCallRequest(value *CallRequest) capability.CallRequest {
	if value == nil {
		return capability.CallRequest{}
	}

	return capability.CallRequest{
		CapabilityName: value.CapabilityName,
		ContentType:    value.ContentType,
		Payload:        fromBytesValue(value.Payload),
	}
}

// ToWireCallResult 转换能力结果
func ToWireCallResult(value capability.CallResult) (*CallResult, error) {
	media, err := ToWireMediaRef(value.Media)
	if err != nil {
		return nil, err
	}

	return &CallResult{ContentType: value.ContentType, Payload: toBytesValue(value.Payload), Media: media}, nil
}

// FromWireCallResult 转换 wire 能力结果
func FromWireCallResult(value *CallResult) (capability.CallResult, error) {
	if value == nil {
		return capability.CallResult{}, nil
	}

	media, err := FromWireMediaRef(value.Media)
	if err != nil {
		return capability.CallResult{}, err
	}

	return capability.CallResult{ContentType: value.ContentType, Payload: fromBytesValue(value.Payload), Media: media}, nil
}

// toApplyStatus 编码配置应用状态，未知值归一为零值
func toApplyStatus(value config.ApplyStatus) ApplyStatus {
	if value < config.ApplyStatusUnknown || value > config.ApplyStatusFailed {
		return ApplyStatus_APPLY_STATUS_UNKNOWN
	}

	return ApplyStatus(value)
}

// fromApplyStatus 解码配置应用状态，未知值归一为零值
func fromApplyStatus(value ApplyStatus) config.ApplyStatus {
	if value < ApplyStatus_APPLY_STATUS_UNKNOWN || value > ApplyStatus_APPLY_STATUS_FAILED {
		return config.ApplyStatusUnknown
	}

	return config.ApplyStatus(value)
}

// ToWireConfigSnapshot 转换配置快照
func ToWireConfigSnapshot(value *config.ConfigSnapshot) *ConfigSnapshot {
	if value == nil {
		return nil
	}

	return &ConfigSnapshot{
		Revision:       value.Revision,
		ActiveRevision: value.ActiveRevision,
		Data:           toBytesValue(value.Data),
		ApplyStatus:    toApplyStatus(value.ApplyStatus),
	}
}

// FromWireConfigSnapshot 转换 wire 配置快照
func FromWireConfigSnapshot(value *ConfigSnapshot) *config.ConfigSnapshot {
	if value == nil {
		return nil
	}

	return &config.ConfigSnapshot{
		Revision:       value.Revision,
		ActiveRevision: value.ActiveRevision,
		Data:           fromBytesValue(value.Data),
		ApplyStatus:    fromApplyStatus(value.ApplyStatus),
	}
}
