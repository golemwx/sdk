package rpc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/plugin"
	"github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// fixturePlugin 只实现必要的 Loader，以真实进程验证可选生命周期
type fixturePlugin struct {
	mode string // 当前进程的受控测试场景
}

// Descriptor 提供固定测试声明，描述失败场景会触发 SDK 的 panic 保护
func (p *fixturePlugin) Descriptor() plugin.Descriptor {
	if p.mode == "describe-failure" {
		panic("private fixture error")
	}
	if p.mode == "missing-event-handler" {
		return plugin.Descriptor{ID: "fixture", Subscriptions: []plugin.Subscription{{Topic: "fixture"}}}
	}
	if p.mode == "missing-command-handler" {
		return plugin.Descriptor{ID: "fixture", Commands: []plugin.CommandSpec{{Path: []string{"fixture"}}}}
	}
	if p.mode == "missing-capability-handler" {
		return plugin.Descriptor{ID: "fixture", Capabilities: []plugin.CapabilitySpec{{Name: "fixture"}}}
	}
	return plugin.Descriptor{ID: "fixture", RequestedPermissions: []sdk.Permission{sdk.PermissionContactRead}}
}

// OnLoad 在反向就绪确认之后使用显式 Host 回调宿主
func (p *fixturePlugin) OnLoad(ctx context.Context, host *plugin.Host) error {
	if _, inherited := os.LookupEnv("GOLEM_TEST_HOST_SECRET"); inherited {
		return errors.New("host environment leaked into plugin")
	}
	if p.mode == "load-failure" {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "fixture.load", "load rejected")
	}
	if p.mode == "authorization-denied" {
		_, err := host.Account.Current(ctx)
		var business *sdk.Error
		if !errors.As(err, &business) || business.Category != sdk.CategoryPermissionDenied {
			return errors.New("missing authorization was not denied")
		}
		return nil
	}
	if p.mode == "missing-host" {
		_, err := host.Account.Current(ctx)
		var business *sdk.Error
		if !errors.As(err, &business) || business.Category != sdk.CategoryUnimplemented {
			return errors.New("missing service was not unimplemented")
		}
		return nil
	}
	if p.mode == "empty-host-result" {
		_, err := host.Account.Current(ctx)
		if !hasCategory(err, sdk.CategoryInternal) {
			return errors.New("empty host result silently succeeded")
		}
		return nil
	}
	account, err := host.Account.Current(ctx)
	if err != nil {
		return err
	}
	if account == nil || account.Username != "wxid_fixture" || !hostPathValid(host) {
		return errors.New("invalid explicit Host or account")
	}
	if p.mode == "cancel-load" {
		<-ctx.Done()
		return ctx.Err()
	}
	if p.mode == "host-contracts" {
		return exerciseHost(ctx, host)
	}
	return nil
}

// hostPathValid 校验初始化确实传入绝对插件数据目录和全部客户端
func hostPathValid(host *plugin.Host) bool {
	return host.DataDir != "" && host.Messages != nil && host.Contacts != nil && host.Chatrooms != nil && host.Sessions != nil && host.Caller != nil && host.Config != nil
}

// TestPluginProcess 在同一测试二进制的独立子进程内运行真实插件
func TestPluginProcess(t *testing.T) {
	mode := os.Getenv("GOLEM_TEST_PLUGIN")
	if mode == "" {
		return
	}
	if mode == "cookie-mismatch" {
		_ = os.Setenv(MagicCookieKey, "wrong-cookie")
	}
	if mode == "version-mismatch" {
		handshake := transport.Handshake()
		handshake.ProtocolVersion++
		hplugin.Serve(&hplugin.ServeConfig{
			HandshakeConfig: handshake, Logger: hclog.NewNullLogger(), GRPCServer: transport.Server,
			Plugins: transport.PluginSet(func(_ *hplugin.GRPCBroker, server *grpc.Server) error {
				wire.RegisterPluginControlServiceServer(server, &wire.UnimplementedPluginControlServiceServer{})
				return nil
			}),
		})
		return
	}
	if mode == "execution" {
		plugin.Serve(&executionPlugin{fixturePlugin: fixturePlugin{mode: mode}, release: make(chan struct{})})
		return
	}
	plugin.Serve(&fixturePlugin{mode: mode})
}

// fixtureAccount 提供独立人工账号数据并观察父调用取消
type fixtureAccount struct {
	calls   atomic.Int32  // 已实际进入宿主业务的调用数
	entered chan struct{} // 可选的显式进入信号
}

// Current 提供必填 UTC 时间，不访问真实账号或磁盘资料
func (a *fixtureAccount) Current(ctx context.Context) (*sdk.Account, error) {
	a.calls.Add(1)
	if a.entered != nil {
		close(a.entered)
	}
	return &sdk.Account{Username: "wxid_fixture", Status: sdk.AccountStatusLoggedIn, UpdatedAt: time.Date(2026, 10, 27, 0, 0, 0, 123, time.UTC)}, nil
}

// fixtureOptions 为独立测试进程创建最小显式环境和宿主输入
func fixtureOptions(t *testing.T, mode string) ClientOptions {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestPluginProcess$", "-test.timeout=30s")
	command.Env = []string{"GOLEM_TEST_PLUGIN=" + mode}
	if root := os.Getenv("SystemRoot"); root != "" {
		command.Env = append(command.Env, "SystemRoot="+root)
	}
	return ClientOptions{
		Command: command, ExpectedID: "fixture", DataDir: t.TempDir(), InstanceGeneration: 1, AccountEpoch: 1,
		Services: HostServices{Host: &plugin.Host{Account: &fixtureAccount{}}, Authorize: func(ctx context.Context, invocation Invocation, method string) (context.Context, error) {
			return ctx, nil
		}},
	}
}

// assertReaped 检查 go-plugin 已等待其拥有的唯一子进程退出
func assertReaped(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.Process == nil || command.ProcessState == nil {
		t.Fatalf("plugin process was not reaped: process=%v state=%v", command.Process, command.ProcessState)
	}
}

// TestStartCloseAndRepeat 验证真实双向 mTLS、启动顺序、可选生命周期及重复启动清理
func TestStartCloseAndRepeat(t *testing.T) {
	t.Setenv("GOLEM_TEST_HOST_SECRET", "must-not-be-inherited")
	for range 3 {
		options := fixtureOptions(t, "normal")
		var ready atomic.Bool
		options.Services.ReverseReady = func(ctx context.Context) error {
			remote, ok := peer.FromContext(ctx)
			if !ok {
				return errors.New("missing TLS peer")
			}
			tls, ok := remote.AuthInfo.(credentials.TLSInfo)
			if !ok || len(tls.State.PeerCertificates) == 0 {
				return errors.New("reverse connection is not mutual TLS")
			}
			ready.Store(true)
			return nil
		}
		options.Services.Authorize = func(ctx context.Context, invocation Invocation, method string) (context.Context, error) {
			if !ready.Load() {
				return nil, errors.New("business callback before readiness")
			}
			return ctx, nil
		}
		client, err := Start(t.Context(), options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		if !ready.Load() {
			t.Fatal("reverse readiness not confirmed")
		}
		if err := client.Enable(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := client.Disable(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := client.Unload(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
		if !client.Exited() {
			t.Fatal("plugin still running")
		}
		select {
		case <-client.serverDone:
		default:
			t.Fatal("reverse server still running")
		}
		assertReaped(t, options.Command)
	}
}

// TestStartupFailuresCleanResources 验证各初始化阶段失败均回收实际进程
func TestStartupFailuresCleanResources(t *testing.T) {
	for _, scenario := range []string{"cookie-mismatch", "version-mismatch", "describe-failure", "identity-mismatch", "validation-failure", "validation-panic", "reverse-failure", "load-failure", "unrequested-permission", "missing-event-handler", "missing-command-handler", "missing-capability-handler"} {
		t.Run(scenario, func(t *testing.T) {
			options := fixtureOptions(t, scenario)
			switch scenario {
			case "identity-mismatch":
				options.ExpectedID = "different"
			case "validation-failure":
				options.Validate = func(context.Context, plugin.Descriptor) error { return errors.New("host policy rejected") }
			case "validation-panic":
				options.Validate = func(context.Context, plugin.Descriptor) error { panic("private host policy panic") }
			case "reverse-failure":
				options.Services.ReverseReady = func(context.Context) error {
					return sdk.NewError(sdk.CategoryUnavailable, "fixture.reverse", "rejected")
				}
			case "unrequested-permission":
				options.GrantedPermissions = []sdk.Permission{sdk.PermissionMessageSend}
			}
			client, err := Start(t.Context(), options)
			if err == nil {
				_ = client.Close()
				t.Fatal("startup unexpectedly succeeded")
			}
			if client != nil {
				t.Fatal("failed startup returned an instance")
			}
			assertReaped(t, options.Command)
		})
	}
}

// TestStartupCancellation 等待 OnLoad 实际进入后取消父调用并确认清理
func TestStartupCancellation(t *testing.T) {
	options := fixtureOptions(t, "cancel-load")
	entered := make(chan struct{})
	options.Services.Host.Account = &fixtureAccount{entered: entered}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		client, err := Start(ctx, options)
		if client != nil {
			_ = client.Close()
		}
		finished <- err
	}()
	select {
	case <-entered:
		cancel()
	case err := <-finished:
		t.Fatalf("load did not enter: %v", err)
	case <-t.Context().Done():
		t.Fatal("test canceled")
	}
	if err := <-finished; err == nil {
		t.Fatal("canceled startup succeeded")
	}
	assertReaped(t, options.Command)
}

// TestHostAuthorizationAndMissingService 验证缺少授权默认拒绝及缺少实现明确失败
func TestHostAuthorizationAndMissingService(t *testing.T) {
	for _, mode := range []string{"authorization-denied", "missing-host"} {
		t.Run(mode, func(t *testing.T) {
			options := fixtureOptions(t, mode)
			account := options.Services.Host.Account.(*fixtureAccount)
			if mode == "authorization-denied" {
				options.Services.Authorize = nil
			} else {
				options.Services.Host = nil
			}
			client, err := Start(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			_ = client.Close()
			assertReaped(t, options.Command)
			if mode == "authorization-denied" && account.calls.Load() != 0 {
				t.Fatal("unauthorized request reached host business implementation")
			}
		})
	}
}

// TestPairingCopies 验证配对配置和 PluginSet 只返回副本
func TestPairingCopies(t *testing.T) {
	first := HandshakeConfig()
	first.ProtocolVersion++
	delete(PluginSet(), PluginKey)
	if HandshakeConfig().ProtocolVersion != ProtocolVersion || len(PluginSet()) != 1 {
		t.Fatal("shared pairing configuration changed")
	}
}
