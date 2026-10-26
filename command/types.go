// Package command 定义命令声明、输入和结果
package command

import (
	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/message"
)

// ArgType 命令参数类型
type ArgType int

const (
	// ArgTypeUnknown 未知命令参数类型
	ArgTypeUnknown ArgType = iota
	// ArgTypeString 字符串参数
	ArgTypeString
	// ArgTypeInt 整数参数
	ArgTypeInt
	// ArgTypeFloat 浮点数参数
	ArgTypeFloat
	// ArgTypeBool 布尔参数
	ArgTypeBool
	// ArgTypeEnum 限定候选值的枚举参数
	ArgTypeEnum
)

// ArgSpec 命令参数声明
type ArgSpec struct {
	// Name 命名参数名称
	Name string
	// Type 命令参数类型
	Type ArgType
	// Required 参数是否必须提供
	Required bool
	// Default 参数缺失时使用的默认值
	Default string
	// Enum 枚举参数允许的候选值
	Enum []string
	// Min 参数最小值，缺失时不设置下界
	Min *float64
	// Max 参数最大值，缺失时不设置上界
	Max *float64
	// Help 面向用户的帮助说明
	Help string
}

// CommandSpec 命令声明
type CommandSpec struct {
	// Path 由各级命令名称组成的路径
	Path []string
	// Args 显式命令参数声明，不通过反射推断
	Args []ArgSpec
	// Permission 执行命令所需权限
	Permission sdk.Permission
	// Help 面向用户的帮助说明
	Help string
}

// Command 已完成词法、权限与参数校验的命令
type Command struct {
	// Path 由各级命令名称组成的路径
	Path []string
	// Args 已完成校验的命名参数值
	Args map[string]string
	// Positional 按输入顺序保留的位置参数
	Positional []string
	// Actor 实际发言者，无法确定或没有发言者时为空
	Actor *sdk.Actor
	// Message 关联的归一化业务消息
	Message *message.Message
	// Raw 原始命令文本
	Raw string
}

// CommandResult 命令执行结果
type CommandResult struct {
	// Replied 命令处理函数是否已经自行回复
	Replied bool
	// Text 文本内容，缺失与显式空字符串可区分
	Text *string
	// Media 宿主管理的媒体引用
	Media *sdk.MediaRef
}
