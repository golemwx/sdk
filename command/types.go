// Package command 定义结构体命令的 schema、调用输入、owner 上下文和回复契约。
package command

import (
	"context"

	"github.com/golemwx/sdk/message"
)

// Scope 是命令执行所需的权限范围。
type Scope uint8

const (
	// ScopeUnknown 表示权限范围未知，调用方应按拒绝处理。
	ScopeUnknown Scope = iota
	// ScopeUser 表示普通用户可执行。
	ScopeUser
	// ScopeOwner 表示仅 owner 可执行，且必须在参数解析前完成身份校验。
	ScopeOwner
)

// ValueType 是命令参数的基础类型。
type ValueType string

const (
	// TypeUnknown 表示命令参数类型未知，调用方应按不支持处理。
	TypeUnknown ValueType = ""
	// TypeString 表示字符串参数。
	TypeString ValueType = "string"
	// TypeInt 表示整数参数。
	TypeInt ValueType = "int"
	// TypeFloat 表示浮点参数。
	TypeFloat ValueType = "float"
	// TypeBool 表示布尔参数。
	TypeBool ValueType = "bool"
	// TypeDuration 表示 time.Duration 参数。
	TypeDuration ValueType = "duration"
	// TypeEnum 表示有限枚举参数。
	TypeEnum ValueType = "enum"
	// TypeMap 表示键值参数。
	TypeMap ValueType = "map"
)

// Argument 是可序列化的命令参数描述。
type Argument struct {
	Name       string
	Short      string
	Positional bool
	Type       ValueType
	Required   bool
	Repeated   bool
	Default    string
	Enum       []string
	Help       string
}

// Node 是可序列化命令树节点。
type Node struct {
	Name      string
	Aliases   []string
	Help      string
	Scope     Scope
	Arguments []Argument
	Children  []Node
}

// Schema 是插件自动上报的完整命令树。
type Schema struct {
	Roots []Node
}

// Invocation 是 frame 交给插件的原始命令词法单元和上下文。
//
// frame 先根据 Schema 选择插件并校验 owner 范围，再发送 Invocation；
// 插件侧 Kong 负责参数绑定。Tokens 保留用户输入顺序，不含前导斜杠。
type Invocation struct {
	Tokens  []string
	Raw     string
	Message *message.Message
	Owner   bool
}

// Reply 是命令处理函数的文本回复入口。
type Reply interface {
	Text(ctx context.Context, text string) error
}

// Authorization 是由 frame 验证后写入调用上下文的 owner 状态。
//
// 插件不得根据消息正文、昵称或群 ID 自行构造该状态。保存插件配置的
// frame handler 必须从同一可信调用上下文读取它。
type Authorization struct {
	Owner bool
}

type authorizationKey struct{}

// WithAuthorization 由 frame 运行时在调用处理函数前写入已验证的权限状态。
func WithAuthorization(ctx context.Context, authorization Authorization) context.Context {
	return context.WithValue(ctx, authorizationKey{}, authorization)
}

// AuthorizationFromContext 读取 frame 写入的 owner 状态。
func AuthorizationFromContext(ctx context.Context) (Authorization, bool) {
	authorization, ok := ctx.Value(authorizationKey{}).(Authorization)
	return authorization, ok
}
