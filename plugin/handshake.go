package plugin

import (
	"github.com/golemwx/sdk/internal/transport"
	hplugin "github.com/hashicorp/go-plugin"
)

const (
	// ProtocolVersion 是 frame 与插件必须一致，且仅在破坏性变更时提高的版本。
	ProtocolVersion = transport.ProtocolVersion
	// MagicCookieKey 是 go-plugin 启动二次确认的环境变量名。
	MagicCookieKey = transport.MagicCookieKey
	// MagicCookieValue 是配对值，不是业务访问凭据。
	MagicCookieValue = transport.MagicCookieValue
	// PluginKey 是 go-plugin PluginSet 中唯一稳定的插件键。
	PluginKey = transport.PluginKey
)

// HandshakeConfig 返回宿主与插件共用的 go-plugin 配对配置。
//
// frame 必须使用完全相同的值创建 ClientConfig；该值不承担授权作用。
func HandshakeConfig() hplugin.HandshakeConfig {
	return transport.Handshake()
}
