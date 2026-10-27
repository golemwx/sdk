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
	Name     string   // 命名参数名称
	Type     ArgType  // 命令参数类型
	Required bool     // 参数是否必须提供
	Default  string   // 参数缺失时使用的默认值
	Enum     []string // 枚举参数允许的候选值
	Min      *float64 // 参数最小值，缺失时不设置下界
	Max      *float64 // 参数最大值，缺失时不设置上界
	Help     string   // 面向用户的帮助说明
}

// CommandSpec 命令声明
type CommandSpec struct {
	Path       []string       // 由各级命令名称组成的路径
	Args       []ArgSpec      // 显式命令参数声明，不通过反射推断
	Permission sdk.Permission // 执行命令所需权限
	Help       string         // 面向用户的帮助说明
}

// Command 已完成词法、权限与参数校验的命令
type Command struct {
	Path       []string          // 由各级命令名称组成的路径
	Args       map[string]string // 已完成校验的命名参数值
	Positional []string          // 按输入顺序保留的位置参数
	Actor      *sdk.Actor        // 实际发言者，无法确定或没有发言者时为空
	Message    *message.Message  // 关联的归一化业务消息
	Raw        string            // 原始命令文本
}

// CommandResult 命令执行结果
type CommandResult struct {
	Replied bool          // 命令处理函数是否已经自行回复
	Text    *string       // 文本内容，缺失与显式空字符串可区分
	Media   *sdk.MediaRef // 宿主管理的媒体引用
}
