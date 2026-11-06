package plugin

import (
	"log/slog"
	"os"

	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// Serve 运行一个插件进程。
//
// Serve 在启动 go-plugin 通信前读取固定元数据并取得安装目录与插件 ID
// 对应的 OS 排他锁。锁竞争、元数据无效或握手失败都会让进程以非零状态
// 退出；锁文件本身不删除，进程退出由操作系统释放锁。
func Serve(implementation Plugin) {
	loggers := setupPluginLogging()
	if err := serve(implementation, loggers); err != nil {
		slog.Error("golem plugin startup failed", "error", err)
		os.Exit(1)
	}
}

func serve(implementation Plugin, loggers *pluginLoggers) error {
	runtime, err := newRuntime(implementation)
	if err != nil {
		return err
	}
	loggers.identity.Set(runtime.metadata.ID, runtime.metadata.Name)
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
		Logger:     loggers.hclog,
	})
	return nil
}
