// Package transport 统一管理 go-plugin 配对及内部连接，不暴露生成类型给业务调用方
package transport

import (
	"context"

	"github.com/golemwx/sdk/internal/wire"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const (
	// ProtocolVersion 唯一通信协议版本，仅不兼容变更时提高
	ProtocolVersion = 1
	// MagicCookieKey 宿主与插件共享的固定配对环境变量名
	MagicCookieKey = "GOLEM_PLUGIN_MAGIC_COOKIE"
	// MagicCookieValue 固定配对值，不作为运行时访问凭据
	MagicCookieValue = "golem-sdk-grpc"
	// PluginKey 宿主和插件唯一的 PluginSet 键
	PluginKey = "golem"
	// MaxMessageSize 单次一元 RPC 的消息字节上限
	MaxMessageSize = 4 << 20
)

// Endpoint 宿主到插件的控制连接和共享反向 Broker
type Endpoint struct {
	// Control 插件控制 RPC 客户端
	Control wire.PluginControlServiceClient
	// Broker 当前实例唯一的反向服务 Broker
	Broker *hplugin.GRPCBroker
}

// Adapter 将内部服务接入固定的 go-plugin 插件集合
type Adapter struct {
	// NetRPCUnsupportedPlugin 禁用 net/rpc 通信
	hplugin.NetRPCUnsupportedPlugin
	// Register 插件侧注册函数，宿主侧为空
	Register func(*hplugin.GRPCBroker, *grpc.Server) error
}

// Handshake 返回独立副本，防止调用方修改共享的配对配置
func Handshake() hplugin.HandshakeConfig {
	return hplugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: MagicCookieKey, MagicCookieValue: MagicCookieValue}
}

// PluginSet 为宿主或插件生成唯一键的插件集合
func PluginSet(register func(*hplugin.GRPCBroker, *grpc.Server) error) hplugin.PluginSet {
	return hplugin.PluginSet{PluginKey: &Adapter{Register: register}}
}

// GRPCServer 注册插件侧控制及业务服务
func (a *Adapter) GRPCServer(broker *hplugin.GRPCBroker, server *grpc.Server) error {
	return a.Register(broker, server)
}

// GRPCClient 构造宿主控制入口，所有 Host 服务共用 Broker
func (a *Adapter) GRPCClient(_ context.Context, broker *hplugin.GRPCBroker, conn *grpc.ClientConn) (interface{}, error) {
	return &Endpoint{Control: wire.NewPluginControlServiceClient(conn), Broker: broker}, nil
}

// Server 保留调用方传入的 TLS 选项并施加消息大小限制
func Server(options []grpc.ServerOption) *grpc.Server {
	return grpc.NewServer(append(options, grpc.MaxRecvMsgSize(MaxMessageSize), grpc.MaxSendMsgSize(MaxMessageSize))...)
}

// DialOptions 为两条连接统一设置消息大小限制
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxMessageSize), grpc.MaxCallSendMsgSize(MaxMessageSize))}
}
