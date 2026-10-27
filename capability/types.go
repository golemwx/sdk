// Package capability 定义跨插件能力调用契约
package capability

import (
	"context"

	"github.com/golemwx/sdk"
)

// CapabilitySpec 插件公开能力声明
type CapabilitySpec struct {
	Name          string // 公开能力名，调用方只按此名称发起调用
	InputType     string // 能力输入内容类型
	OutputType    string // 能力输出内容类型
	Description   string // 面向使用者的业务说明
	MaxInlineSize int    // 能力内联结果字节上限，0 使用默认 256 KiB，不得大于 256 KiB
}

// CallRequest 跨插件能力调用请求
type CallRequest struct {
	CapabilityName string            // 目标能力名，由宿主选择提供者，不指定插件 ID
	ContentType    string            // 载荷内容类型
	Payload        []byte            // 内联业务载荷，缺失与显式空字节可区分
	Actor          *sdk.Actor        // 实际发言者，无法确定或没有发言者时为空
	Conversation   *sdk.Conversation // 所属或目标业务会话，群内所有成员共用整群会话
}

// CallResult 能力调用结果
type CallResult struct {
	ContentType string        // 载荷内容类型
	Payload     []byte        // 内联结果载荷，与 Media 互斥，缺失与显式空字节可区分
	Media       *sdk.MediaRef // 宿主管理的结果媒体引用，与 Payload 互斥
}

// CallerClient 宿主跨插件调用能力
type CallerClient interface {
	Call(ctx context.Context, request CallRequest) (*CallResult, error)
}
