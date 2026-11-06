package wire

import (
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
)

// ToWireCommandSchema 转换自动命令树。
func ToWireCommandSchema(value command.Schema) *CommandSchema {
	result := &CommandSchema{Roots: make([]*CommandNode, len(value.Roots))}
	for index, node := range value.Roots {
		result.Roots[index] = ToWireCommandNode(node)
	}
	return result
}

// FromWireCommandSchema 转换自动命令树。
func FromWireCommandSchema(value *CommandSchema) command.Schema {
	if value == nil {
		return command.Schema{}
	}
	result := command.Schema{Roots: make([]command.Node, len(value.GetRoots()))}
	for index, node := range value.GetRoots() {
		result.Roots[index] = FromWireCommandNode(node)
	}
	return result
}

// ToWireCommandNode 转换命令树节点。
func ToWireCommandNode(value command.Node) *CommandNode {
	result := &CommandNode{
		Name:      value.Name,
		Aliases:   cloneStrings(value.Aliases),
		Help:      value.Help,
		Scope:     toWireCommandScope(value.Scope),
		Arguments: make([]*CommandArgument, len(value.Arguments)),
		Children:  make([]*CommandNode, len(value.Children)),
	}
	for index, argument := range value.Arguments {
		result.Arguments[index] = ToWireCommandArgument(argument)
	}
	for index, child := range value.Children {
		result.Children[index] = ToWireCommandNode(child)
	}
	return result
}

// FromWireCommandNode 转换命令树节点。
func FromWireCommandNode(value *CommandNode) command.Node {
	if value == nil {
		return command.Node{}
	}
	result := command.Node{
		Name:      value.GetName(),
		Aliases:   cloneStrings(value.GetAliases()),
		Help:      value.GetHelp(),
		Scope:     fromWireCommandScope(value.GetScope()),
		Arguments: make([]command.Argument, len(value.GetArguments())),
		Children:  make([]command.Node, len(value.GetChildren())),
	}
	for index, argument := range value.GetArguments() {
		result.Arguments[index] = FromWireCommandArgument(argument)
	}
	for index, child := range value.GetChildren() {
		result.Children[index] = FromWireCommandNode(child)
	}
	return result
}

// ToWireCommandArgument 转换命令参数描述。
func ToWireCommandArgument(value command.Argument) *CommandArgument {
	return &CommandArgument{
		Name:         value.Name,
		Short:        value.Short,
		Positional:   value.Positional,
		Type:         string(value.Type),
		Required:     value.Required,
		Repeated:     value.Repeated,
		DefaultValue: value.Default,
		EnumValues:   cloneStrings(value.Enum),
		Help:         value.Help,
	}
}

// FromWireCommandArgument 转换命令参数描述。
func FromWireCommandArgument(value *CommandArgument) command.Argument {
	if value == nil {
		return command.Argument{}
	}
	return command.Argument{
		Name:       value.GetName(),
		Short:      value.GetShort(),
		Positional: value.GetPositional(),
		Type:       command.ValueType(value.GetType()),
		Required:   value.GetRequired(),
		Repeated:   value.GetRepeated(),
		Default:    value.GetDefaultValue(),
		Enum:       cloneStrings(value.GetEnumValues()),
		Help:       value.GetHelp(),
	}
}

func toWireCommandScope(value command.Scope) CommandScope {
	switch value {
	case command.ScopeUser:
		return CommandScope_COMMAND_SCOPE_USER
	case command.ScopeOwner:
		return CommandScope_COMMAND_SCOPE_OWNER
	default:
		return CommandScope_COMMAND_SCOPE_UNKNOWN
	}
}

func fromWireCommandScope(value CommandScope) command.Scope {
	switch value {
	case CommandScope_COMMAND_SCOPE_USER:
		return command.ScopeUser
	case CommandScope_COMMAND_SCOPE_OWNER:
		return command.ScopeOwner
	default:
		return command.ScopeUnknown
	}
}

// ToWireCommandInvocation 转换命令调用输入。
func ToWireCommandInvocation(value command.Invocation) (*CommandInvocation, error) {
	messageValue, err := ToWireMessage(value.Message)
	if err != nil {
		return nil, err
	}
	return &CommandInvocation{
		Tokens:  cloneStrings(value.Tokens),
		Raw:     value.Raw,
		Message: messageValue,
		Owner:   value.Owner,
	}, nil
}

// FromWireCommandInvocation 转换命令调用输入。
func FromWireCommandInvocation(value *CommandInvocation) (*command.Invocation, error) {
	if value == nil {
		return nil, nil
	}
	messageValue, err := FromWireMessage(value.GetMessage())
	if err != nil {
		return nil, err
	}
	return &command.Invocation{
		Tokens:  cloneStrings(value.GetTokens()),
		Raw:     value.GetRaw(),
		Message: messageValue,
		Owner:   value.GetOwner(),
	}, nil
}

// ToWireCapabilitySpec 转换能力声明。
func ToWireCapabilitySpec(value capability.Spec) (*CapabilitySpec, error) {
	if value.MaxInlineSize < 0 || value.MaxInlineSize > capability.MaxInlineSize {
		return nil, invalidArgument("plugin.capability.max_inline_size", "inline size is outside the supported range")
	}
	return &CapabilitySpec{
		Name:          value.Name,
		Description:   value.Description,
		InputType:     value.InputType,
		OutputType:    value.OutputType,
		MaxInlineSize: int32(value.MaxInlineSize),
	}, nil
}

// FromWireCapabilitySpec 转换能力声明。
func FromWireCapabilitySpec(value *CapabilitySpec) capability.Spec {
	if value == nil {
		return capability.Spec{}
	}
	return capability.Spec{
		Name:          value.GetName(),
		Description:   value.GetDescription(),
		InputType:     value.GetInputType(),
		OutputType:    value.GetOutputType(),
		MaxInlineSize: int(value.GetMaxInlineSize()),
	}
}

// ToWireCapabilityRequest 转换能力请求。
func ToWireCapabilityRequest(value capability.Request) *CapabilityRequest {
	return &CapabilityRequest{
		Name:        value.Name,
		ContentType: value.ContentType,
		Payload:     ToWireBytes(value.Payload),
	}
}

// FromWireCapabilityRequest 转换能力请求。
func FromWireCapabilityRequest(value *CapabilityRequest) capability.Request {
	if value == nil {
		return capability.Request{}
	}
	return capability.Request{
		Name:        value.GetName(),
		ContentType: value.GetContentType(),
		Payload:     FromWireBytes(value.GetPayload()),
	}
}

// ToWireCapabilityResult 转换能力结果。
func ToWireCapabilityResult(value capability.Result) *CapabilityResult {
	return &CapabilityResult{
		ContentType: value.ContentType,
		Payload:     ToWireBytes(value.Payload),
	}
}

// FromWireCapabilityResult 转换能力结果。
func FromWireCapabilityResult(value *CapabilityResult) capability.Result {
	if value == nil {
		return capability.Result{}
	}
	return capability.Result{
		ContentType: value.GetContentType(),
		Payload:     FromWireBytes(value.GetPayload()),
	}
}
