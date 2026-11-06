package plugin

import (
	"fmt"
	"os"

	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	hclog "github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// Serve 运行一个插件进程。
//
// Serve 在启动 go-plugin 通信前读取固定元数据并取得安装目录与插件 ID
// 对应的 OS 排他锁。锁竞争、元数据无效或握手失败都会让进程以非零状态
// 退出；锁文件本身不删除，进程退出由操作系统释放锁。
func Serve(implementation Plugin) {
	if err := serve(implementation); err != nil {
		fmt.Fprintf(os.Stderr, "golem plugin: %s\n", err.Error())
		os.Exit(1)
	}
}

func serve(implementation Plugin) error {
	runtime, err := newRuntime(implementation)
	if err != nil {
		return err
	}
	instanceLock, err := acquireInstanceLock(runtime.metadata.ID)
	if err != nil {
		return err
	}
	defer closeInstanceLock(instanceLock)

	control := newControlServer(runtime)
	business := &businessServer{runtime: runtime}
	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: transport.Handshake(),
		Plugins: transport.PluginSet(func(broker *hplugin.GRPCBroker, server *grpc.Server) error {
			control.broker = broker
			wire.RegisterPluginControlServiceServer(server, control)
			wire.RegisterPluginBusinessServiceServer(server, business)
			return nil
		}),
		GRPCServer: transport.Server,
		Logger:     hclog.NewNullLogger(),
	})
	return nil
}
