// Package capability 定义跨插件能力声明和内存内调用契约。
package capability

import "context"

// MaxInlineSize 是单次能力请求或结果允许的内联字节上限。
//
// 大媒体结果不得进入能力载荷或 frame 媒体仓库；提供者应直接通过
// message.Client 发送，或等待后续 SDK 流式能力契约。
const MaxInlineSize = 256 << 10

// Spec 是插件公开能力的描述。
type Spec struct {
	Name          string
	Description   string
	InputType     string
	OutputType    string
	MaxInlineSize int
}

// Request 是按能力名发起的调用请求。
type Request struct {
	Name        string
	ContentType string
	Payload     []byte
}

// Result 是有界内联调用结果。
type Result struct {
	ContentType string
	Payload     []byte
}

// Client 是宿主跨插件能力调用入口。
type Client interface {
	Call(ctx context.Context, request Request) (*Result, error)
}
