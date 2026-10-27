// Package rpc 提供 frame 与插件接线所需的稳定通信参数
package rpc

import "github.com/golemwx/sdk/internal/transport"

const (
	// MaxUnaryMessageSize 一元 RPC 消息上限
	MaxUnaryMessageSize = transport.MaxMessageSize
	// MaxInlinePayloadSize 普通能力内联载荷上限
	MaxInlinePayloadSize = transport.MaxInlinePayloadSize
	// MaxMediaChunkSize 媒体分块上限
	MaxMediaChunkSize = 256 << 10
)
