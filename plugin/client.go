package plugin

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	hclog "github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const defaultPluginStartTimeout = 10 * time.Second

// ClientOptions 是启动一个真实插件子进程所需的显式输入。
//
// Command 必须尚未启动。Command.Env 可携带 GOLEM_PLUGIN_LOCK_PATH 或
// GOLEM_PLUGIN_INSTALL_DIR；未设置时 SDK 从插件可执行文件推导安装根目录。
type ClientOptions struct {
	Command      *exec.Cmd
	PluginID     string
	Host         *Host
	ConfigClient ConfigClient
	Config       []byte
	DataRoot     string
	StartTimeout time.Duration
	// Logger 接收插件 stderr 上的结构化日志。frame 应传入其启动时配置的
	// slog logger；nil 表示丢弃插件日志。
	Logger *slog.Logger
}

// Client 拥有一个真实插件子进程及其唯一反向 Host 服务。
//
// 调用方必须显式 Close 或 Unload。Unload 返回前会确认子进程退出；
// Client.Close 也会阻塞到操作系统报告进程已经结束。
type Client struct {
	process    *hplugin.Client
	endpoint   *transport.Endpoint
	descriptor *wire.Descriptor
	server     *grpc.Server
	serverDone chan struct{}

	mu        sync.Mutex
	closeOnce sync.Once
	closed    bool
}

// Start 启动插件子进程，完成 Describe、反向 Host 注册和 Initialize。
//
// Start 不调用 Load；调用方按 Load、Start、Stop、Unload 的顺序管理实例。
func Start(ctx context.Context, options ClientOptions) (_ *Client, err error) {
	if err := contextError(ctx, "plugin.start"); err != nil {
		return nil, err
	}
	if options.Command == nil || options.Command.Process != nil || strings.TrimSpace(options.Command.Path) == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "plugin command is invalid")
	}
	if !validPluginID(options.PluginID) {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "expected plugin ID is invalid")
	}
	if options.Host == nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "host is nil")
	}
	if strings.TrimSpace(options.DataRoot) == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "plugin data root is empty")
	}
	if options.StartTimeout < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "start timeout is negative")
	}
	if options.StartTimeout == 0 {
		options.StartTimeout = defaultPluginStartTimeout
	}
	startCtx, cancel := context.WithTimeout(ctx, options.StartTimeout)
	defer cancel()

	var logIdentity *logIdentity
	clientLogger := hclog.NewNullLogger()
	var syncStdout, syncStderr io.Writer
	if options.Logger != nil {
		logIdentity = newLogIdentity(options.PluginID, "")
		handler := &identityHandler{next: options.Logger.Handler(), identity: logIdentity}
		clientLogger = newHclogSlogLogger(slog.New(handler))
		syncStderr = newStreamLogWriter(
			clientLogger.With("stream", "stderr"),
			hclog.Warn,
		)
		syncStdout = newStreamLogWriter(
			clientLogger.With("stream", "stdout"),
			hclog.Error,
		)
	}

	client := &Client{process: hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig:  transport.Handshake(),
		Plugins:          transport.PluginSet(nil),
		Cmd:              options.Command,
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolGRPC},
		AutoMTLS:         true,
		StartTimeout:     options.StartTimeout,
		Logger:           clientLogger,
		SyncStdout:       syncStdout,
		SyncStderr:       syncStderr,
		GRPCDialOptions:  transport.DialOptions(),
	})}
	defer func() {
		if err != nil {
			_ = client.Close()
		}
	}()

	protocol, err := startPluginProtocol(startCtx, client.process)
	if err != nil {
		return nil, err
	}
	dispensed, err := protocol.Dispense(PluginKey)
	if err != nil {
		return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.start", "plugin endpoint dispense failed")
	}
	endpoint, ok := dispensed.(*transport.Endpoint)
	if !ok || endpoint == nil || endpoint.Control == nil || endpoint.Business == nil || endpoint.Broker == nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.start", "plugin endpoint is invalid")
	}
	client.endpoint = endpoint

	described, callErr := endpoint.Control.Describe(startCtx, &emptypb.Empty{})
	if err := transport.DecodeError(described.GetError(), callErr, "plugin.describe"); err != nil {
		return nil, err
	}
	if described.GetDescriptor_() == nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.describe", "plugin descriptor is missing")
	}
	client.descriptor = described.GetDescriptor_()
	if client.descriptor.GetMetadata().GetId() != options.PluginID {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.describe", "plugin descriptor ID does not match expected ID")
	}
	if logIdentity != nil {
		logIdentity.SetName(client.descriptor.GetMetadata().GetName())
	}

	brokerID := endpoint.Broker.NextId()
	client.serverDone = make(chan struct{})
	serverReady := make(chan struct{})
	go func() {
		defer close(client.serverDone)
		endpoint.Broker.AcceptAndServe(brokerID, func(secure []grpc.ServerOption) *grpc.Server {
			server := transport.Server(secure)
			registerHostServer(server, options.Host, options.ConfigClient)
			client.mu.Lock()
			client.server = server
			client.mu.Unlock()
			close(serverReady)
			return server
		})
	}()
	select {
	case <-serverReady:
	case <-client.serverDone:
		return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.start", "reverse host listener failed")
	case <-startCtx.Done():
		return nil, contextError(startCtx, "plugin.start")
	}

	initialized, callErr := endpoint.Control.Initialize(startCtx, &wire.InitializeRequest{
		BrokerId: brokerID,
		Config:   wire.ToWireBytes(options.Config),
		DataRoot: options.DataRoot,
	})
	if err := transport.DecodeError(initialized.GetError(), callErr, "plugin.initialize"); err != nil {
		return nil, err
	}
	return client, nil
}

func startPluginProtocol(ctx context.Context, process *hplugin.Client) (hplugin.ClientProtocol, error) {
	type result struct {
		protocol hplugin.ClientProtocol
		err      error
	}
	started := make(chan result, 1)
	go func() {
		protocol, err := process.Client()
		started <- result{protocol: protocol, err: err}
	}()
	select {
	case value := <-started:
		if value.err != nil {
			return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.start", "plugin handshake failed")
		}
		return value.protocol, nil
	case <-ctx.Done():
		process.Kill()
		<-started
		return nil, contextError(ctx, "plugin.start")
	}
}

// Descriptor 返回启动时校验过的插件描述副本。
func (c *Client) Descriptor() Descriptor {
	if c == nil || c.descriptor == nil {
		return Descriptor{}
	}
	return fromWireDescriptor(c.descriptor)
}

// Load 调用插件可选 OnLoad。
func (c *Client) Load(ctx context.Context) error {
	return c.controlOperation(ctx, "plugin.load", func(requestCtx context.Context) (*wire.OperationResponse, error) {
		return c.endpoint.Control.Load(requestCtx, &emptypb.Empty{})
	})
}

// Start 建立插件新的启用周期并调用可选 OnStart。
func (c *Client) Start(ctx context.Context) error {
	return c.controlOperation(ctx, "plugin.start", func(requestCtx context.Context) (*wire.OperationResponse, error) {
		return c.endpoint.Control.Start(requestCtx, &emptypb.Empty{})
	})
}

// Stop 取消插件启用周期并等待真实在途 handler 退出。
func (c *Client) Stop(ctx context.Context) error {
	return c.controlOperation(ctx, "plugin.stop", func(requestCtx context.Context) (*wire.OperationResponse, error) {
		return c.endpoint.Control.Stop(requestCtx, &emptypb.Empty{})
	})
}

// Unload 调用 OnUnload，然后终止并等待子进程退出。
//
// 即使 Unload RPC 超时或被取消，子进程仍会被终止。返回 nil 表示卸载
// 调用成功且操作系统已确认进程退出；否则调用方必须检查 Exited()，
// 未确认退出前不得启动同 ID 的替代进程。
func (c *Client) Unload(ctx context.Context) error {
	if c == nil {
		return nil
	}
	var unloadErr error
	if !c.isClosed() && c.endpoint != nil {
		unloadErr = c.controlOperation(ctx, "plugin.unload", func(requestCtx context.Context) (*wire.OperationResponse, error) {
			return c.endpoint.Control.Unload(requestCtx, &emptypb.Empty{})
		})
	}
	return errors.Join(unloadErr, c.Close())
}

// Close 停止反向 Host 服务并同步等待子进程退出，可重复调用。
//
// 只有 go-plugin 已确认进程退出时才返回 nil。
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.closeHostServer()
		if c.process != nil {
			c.process.Kill()
		}
		if c.serverDone != nil {
			<-c.serverDone
		}
	})
	if !c.Exited() {
		return sdk.NewError(sdk.CategoryUnavailable, "plugin.close", "plugin process exit was not confirmed")
	}
	return nil
}

func (c *Client) closeHostServer() {
	if c == nil {
		return
	}
	c.mu.Lock()
	server := c.server
	c.mu.Unlock()
	if server != nil {
		server.Stop()
	}
}

// Exited 报告 go-plugin 是否已确认子进程退出。
func (c *Client) Exited() bool {
	if c == nil || c.process == nil {
		return true
	}
	return c.process.Exited()
}

// PID 返回 go-plugin 当前子进程标识；未启动时为空。
func (c *Client) PID() string {
	if c == nil || c.process == nil {
		return ""
	}
	return c.process.ID()
}

// ValidateConfig 调用插件的候选配置校验入口。
func (c *Client) ValidateConfig(ctx context.Context, config []byte) error {
	if err := c.ready(ctx, "plugin.config.validate"); err != nil {
		return err
	}
	response, callErr := c.endpoint.Control.ValidateConfig(ctx, &wire.ValidateConfigRequest{
		Candidate: wire.ToWireBytes(config),
	})
	return transport.DecodeError(response.GetError(), callErr, "plugin.config.validate")
}

// SetConfig 调用插件配置发布入口。
func (c *Client) SetConfig(ctx context.Context, config []byte) error {
	if err := c.ready(ctx, "plugin.config.set"); err != nil {
		return err
	}
	response, callErr := c.endpoint.Control.SetConfig(ctx, &wire.SetConfigRequest{
		Config: wire.ToWireBytes(config),
	})
	return transport.DecodeError(response.GetError(), callErr, "plugin.config.set")
}

// HandleEvent 投递一次瞬时事件。
func (c *Client) HandleEvent(ctx context.Context, value event.Event) (event.Result, error) {
	if err := c.ready(ctx, "plugin.event"); err != nil {
		return event.Result{}, err
	}
	encoded, err := wire.ToWireEvent(value)
	if err != nil {
		return event.Result{}, err
	}
	response, callErr := c.endpoint.Business.HandleEvent(ctx, &wire.HandleEventRequest{Event: encoded})
	if err := transport.DecodeError(response.GetError(), callErr, "plugin.event"); err != nil {
		return event.Result{}, err
	}
	return wire.FromWireEventResult(response.GetResult()), nil
}

// HandleCommand 投递一次 frame 已完成路由的命令。
func (c *Client) HandleCommand(ctx context.Context, invocation command.Invocation) error {
	if err := c.ready(ctx, "plugin.command"); err != nil {
		return err
	}
	encoded, err := wire.ToWireCommandInvocation(invocation)
	if err != nil {
		return err
	}
	response, callErr := c.endpoint.Business.HandleCommand(ctx, &wire.HandleCommandRequest{Command: encoded})
	return transport.DecodeError(response.GetError(), callErr, "plugin.command")
}

// HandleCapability 调用一次插件能力。
func (c *Client) HandleCapability(ctx context.Context, request capability.Request) (capability.Result, error) {
	if err := c.ready(ctx, "plugin.capability"); err != nil {
		return capability.Result{}, err
	}
	response, callErr := c.endpoint.Business.HandleCapability(ctx, &wire.HandleCapabilityRequest{
		Request: wire.ToWireCapabilityRequest(request),
	})
	if err := transport.DecodeError(response.GetError(), callErr, "plugin.capability"); err != nil {
		return capability.Result{}, err
	}
	return wire.FromWireCapabilityResult(response.GetResult()), nil
}

func (c *Client) controlOperation(ctx context.Context, operation string, call func(context.Context) (*wire.OperationResponse, error)) error {
	if err := c.ready(ctx, operation); err != nil {
		return err
	}
	response, callErr := call(ctx)
	return transport.DecodeError(response.GetError(), callErr, operation)
}

func (c *Client) ready(ctx context.Context, operation string) error {
	if c == nil || c.endpoint == nil {
		return sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin client is not initialized")
	}
	if err := contextError(ctx, operation); err != nil {
		return err
	}
	if c.isClosed() {
		return sdk.NewError(sdk.CategoryFailedPrecondition, operation, "plugin client is closed")
	}
	return nil
}

func (c *Client) isClosed() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

var _ Runtime = (*runtime)(nil)
