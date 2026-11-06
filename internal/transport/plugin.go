// Package transport 封装 go-plugin 配对和内部 gRPC 限制，不向业务暴露生成类型。
package transport

import (
	"context"

	"github.com/golemwx/sdk/internal/wire"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const (
	// ProtocolVersion 是当前唯一通信协议版本，不兼容变更时才提高。
	ProtocolVersion uint = 1
	// MagicCookieKey 是 go-plugin 启动二次确认的环境变量名。
	MagicCookieKey = "GOLEM_PLUGIN_MAGIC_COOKIE"
	// MagicCookieValue 是与宿主配对的固定值，不是业务访问凭据。
	MagicCookieValue = "golem-sdk-grpc"
	// PluginKey 是 PluginSet 中唯一稳定的插件键。
	PluginKey = "golem"
	// MaxMessageBytes 是单次一元 RPC 的收发上限。
	MaxMessageBytes = 20 << 20
	// MaxMediaChunkBytes 是媒体流单块上限。
	MaxMediaChunkBytes = 256 << 10
)

// Endpoint 是宿主侧 go-plugin 客户端获得的控制、业务和关联 Broker。
type Endpoint struct {
	Control  wire.PluginControlServiceClient
	Business wire.PluginBusinessServiceClient
	Broker   *hplugin.GRPCBroker
}

// Adapter 将一个 go-plugin 插件集转换为 SDK 的 gRPC 服务。
type Adapter struct {
	hplugin.NetRPCUnsupportedPlugin

	Register func(*hplugin.GRPCBroker, *grpc.Server) error
}

// Handshake 返回独立副本，调用方不能改写共享配对值。
func Handshake() hplugin.HandshakeConfig {
	return hplugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   MagicCookieKey,
		MagicCookieValue: MagicCookieValue,
	}
}

// PluginSet 生成宿主和插件共用的唯一键插件集合。
func PluginSet(register func(*hplugin.GRPCBroker, *grpc.Server) error) hplugin.PluginSet {
	return hplugin.PluginSet{PluginKey: &Adapter{Register: register}}
}

// GRPCServer 只在插件进程执行注册，宿主侧不会调用。
func (a *Adapter) GRPCServer(broker *hplugin.GRPCBroker, server *grpc.Server) error {
	if a == nil || a.Register == nil {
		return nil
	}
	return a.Register(broker, server)
}

// GRPCClient 构造宿主侧的 SDK 控制入口。
func (a *Adapter) GRPCClient(_ context.Context, broker *hplugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return &Endpoint{
		Control:  wire.NewPluginControlServiceClient(conn),
		Business: wire.NewPluginBusinessServiceClient(conn),
		Broker:   broker,
	}, nil
}

// Server 保留 go-plugin 提供的 TLS 选项并施加统一消息上限。
func Server(options []grpc.ServerOption) *grpc.Server {
	options = append(options,
		grpc.MaxRecvMsgSize(MaxMessageBytes),
		grpc.MaxSendMsgSize(MaxMessageBytes),
	)
	return grpc.NewServer(options...)
}

// DialOptions 为反向 Host 连接施加统一消息上限。
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(MaxMessageBytes),
			grpc.MaxCallSendMsgSize(MaxMessageBytes),
		),
	}
}
