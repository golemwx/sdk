package rpc

import (
	"github.com/golemwx/sdk/internal/transport"
	hplugin "github.com/hashicorp/go-plugin"
)

const (
	// ProtocolVersion 唯一通信协议版本，仅不兼容变更时提高
	ProtocolVersion = transport.ProtocolVersion
	// MagicCookieKey 固定配对环境变量名
	MagicCookieKey = transport.MagicCookieKey
	// MagicCookieValue 固定配对值，不是运行权限凭据
	MagicCookieValue = transport.MagicCookieValue
	// PluginKey 固定 PluginSet 键
	PluginKey = transport.PluginKey
)

// HandshakeConfig 返回 SDK 统一配对配置的独立副本
func HandshakeConfig() hplugin.HandshakeConfig { return transport.Handshake() }

// PluginSet 返回宿主使用的 SDK 统一插件集合
func PluginSet() hplugin.PluginSet { return transport.PluginSet(nil) }
