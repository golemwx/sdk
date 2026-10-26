package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/config"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/golemwx/sdk/plugin"
	"github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ClientOptions 启动单个插件实例所需的显式输入
type ClientOptions struct {
	// Command 尚未启动的插件命令，Env 仅填写明确允许的环境变量
	Command *exec.Cmd
	// ExpectedID 由宿主根据插件文件名确定的预期固定标识
	ExpectedID string
	// DataDir 插件自身数据目录，必须为绝对路径
	DataDir string
	// GrantedPermissions 宿主明确授予的权限，必须属于插件请求的权限
	GrantedPermissions []sdk.Permission
	// Config 插件初始化时的业务配置快照
	Config *config.ConfigSnapshot
	// InstanceGeneration 当前实例代次，必须非零
	InstanceGeneration uint64
	// AccountEpoch 当前账号运行代次
	AccountEpoch uint64
	// Services 已绑定当前实例的宿主实现和授权策略
	Services HostServices
	// Validate 在反向服务启动之前执行宿主声明与策略校验
	Validate func(context.Context, plugin.Descriptor) error
	// StartTimeout 整个握手至 OnLoad 的最大时长，零值使用 10 秒
	StartTimeout time.Duration
	// Logger 接收 go-plugin 受控日志，未提供时丢弃
	Logger hclog.Logger
}

// Client 拥有一个插件子进程及其唯一的反向服务，必须显式 Close
type Client struct {
	// process go-plugin 子进程及正向连接的所有者
	process *hplugin.Client
	// endpoint 插件控制面和实例 Broker
	endpoint *transport.Endpoint
	// descriptor 已通过宿主校验的固定声明
	descriptor plugin.Descriptor
	// control 当前实例的反向就绪确认状态
	control *hostControl
	// server 唯一反向 gRPC 服务
	server *grpc.Server
	// serverDone 反向 Broker 服务实际退出信号，启动失败时也会关闭
	serverDone chan struct{}
	// closeOnce 保证重复 Close 只清理一次
	closeOnce sync.Once
}

// Start 严格按握手、描述校验、反向服务、初始化、就绪确认、OnLoad 启动实例
func Start(ctx context.Context, options ClientOptions) (_ *Client, err error) {
	if options.Command == nil || options.Command.Process != nil || options.Command.Path == "" || !filepath.IsAbs(options.DataDir) || options.InstanceGeneration == 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "invalid command, data directory or instance generation")
	}
	if options.StartTimeout < 0 {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.start", "negative startup timeout")
	}
	if options.StartTimeout == 0 {
		options.StartTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, options.StartTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return nil, transport.DecodeError(nil, ctx.Err(), "plugin.start")
	}
	deadline, _ := ctx.Deadline()
	options.StartTimeout = time.Until(deadline)
	if options.Logger == nil {
		options.Logger = hclog.NewNullLogger()
	}
	client := &Client{process: hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig: HandshakeConfig(), Plugins: PluginSet(), Cmd: options.Command,
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolGRPC}, AutoMTLS: true, SkipHostEnv: true,
		StartTimeout: options.StartTimeout, Logger: options.Logger, GRPCDialOptions: transport.DialOptions(),
	})}
	defer func() {
		if recover() != nil {
			err = sdk.NewError(sdk.CategoryInternal, "plugin.start", "startup callback panicked")
		}
		if err != nil {
			_ = client.Close()
		}
	}()

	// go-plugin 的启动 API 没有 context；取消时回收进程并等待该调用实际退出
	started := make(chan error, 1)
	go func() {
		protocol, connectErr := client.process.Client()
		if connectErr == nil {
			var value interface{}
			value, connectErr = protocol.Dispense(PluginKey)
			if connectErr == nil {
				var ok bool
				client.endpoint, ok = value.(*transport.Endpoint)
				if !ok {
					connectErr = sdk.NewError(sdk.CategoryInternal, "plugin.start", "unexpected SDK endpoint")
				}
			}
		}
		started <- connectErr
	}()
	select {
	case err = <-started:
		if err != nil {
			return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.start", "plugin handshake failed")
		}
	case <-ctx.Done():
		client.process.Kill()
		<-started
		return nil, transport.DecodeError(nil, ctx.Err(), "plugin.start")
	}
	response, err := client.endpoint.Control.Describe(ctx, &emptypb.Empty{})
	if err := transport.DecodeError(response.GetError(), err, "plugin.describe"); err != nil {
		return nil, err
	}
	if response.GetDescriptor_() == nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.describe", "missing descriptor")
	}
	client.descriptor = wire.FromWireDescriptor(response.Descriptor_)
	if err := validateDescriptor(options, client.descriptor); err != nil {
		return nil, err
	}
	if options.Validate != nil {
		if err := options.Validate(ctx, client.Descriptor()); err != nil {
			return nil, err
		}
	}

	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	client.control = &hostControl{token: hex.EncodeToString(secret[:]), ready: make(chan struct{}), validate: options.Services.ReverseReady}
	client.serverDone = make(chan struct{})
	serverReady := make(chan struct{})
	brokerID := client.endpoint.Broker.NextId()
	go func() {
		defer close(client.serverDone)
		client.endpoint.Broker.AcceptAndServe(brokerID, func(secure []grpc.ServerOption) *grpc.Server {
			client.server = transport.Server(append(secure, grpc.UnaryInterceptor(hostInterceptor(options.Services))))
			registerHost(client.server, options.Services, client.control)
			close(serverReady)
			return client.server
		})
	}()
	select {
	case <-serverReady:
	case <-client.serverDone:
		return nil, sdk.NewError(sdk.CategoryUnavailable, "plugin.start", "reverse listener failed")
	case <-ctx.Done():
		// 等待 Broker 回调退出后再读取 server，避免清理与构造并发
		client.process.Kill()
		<-client.serverDone
		return nil, transport.DecodeError(nil, ctx.Err(), "plugin.start")
	}
	permissions := make([]string, 0, len(options.GrantedPermissions))
	for _, permission := range options.GrantedPermissions {
		permissions = append(permissions, string(permission))
	}
	initialized, err := client.endpoint.Control.Initialize(ctx, &wire.InitializeRequest{
		BrokerId: brokerID, GrantedPermissions: permissions, Config: wire.ToWireConfigSnapshot(options.Config), DataDir: options.DataDir,
		ControlToken: client.control.token, InstanceGeneration: options.InstanceGeneration, AccountEpoch: options.AccountEpoch,
	})
	if err := transport.DecodeError(initialized.GetError(), err, "plugin.initialize"); err != nil {
		return nil, err
	}
	select {
	case <-client.control.ready:
	default:
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "reverse connection was not confirmed")
	}
	if err := client.lifecycle(ctx, "load"); err != nil {
		return nil, err
	}
	return client, nil
}

// validateDescriptor 检查标识及显式授权边界，宿主策略仍由 Validate 执行
func validateDescriptor(options ClientOptions, descriptor plugin.Descriptor) error {
	if !validPluginID(descriptor.ID) {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "invalid plugin ID")
	}
	if descriptor.ID != options.ExpectedID {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.describe", "descriptor ID does not match expected plugin")
	}
	for _, permission := range options.GrantedPermissions {
		if !slices.Contains(descriptor.RequestedPermissions, permission) {
			return sdk.NewError(sdk.CategoryPermissionDenied, "plugin.initialize", "permission was not requested")
		}
	}
	return nil
}

// validPluginID 校验固定 ASCII 标识，不为每次插件启动编译正则
func validPluginID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for index := range len(id) {
		c := id[index]
		if c != '_' && c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Descriptor 返回固定声明的深副本，防止宿主调用方修改实例状态
func (c *Client) Descriptor() plugin.Descriptor {
	return wire.FromWireDescriptor(wire.ToWireDescriptor(c.descriptor))
}

// lifecycle 发送生命周期控制请求，控制面不占业务执行额度
func (c *Client) lifecycle(ctx context.Context, method string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request := &wire.LifecycleRequest{}
	var response *wire.OperationResponse
	var err error
	switch method {
	case "load":
		response, err = c.endpoint.Control.OnLoad(ctx, request)
	case "enable":
		response, err = c.endpoint.Control.OnEnable(ctx, request)
	case "disable":
		response, err = c.endpoint.Control.OnDisable(ctx, request)
	case "unload":
		response, err = c.endpoint.Control.OnUnload(ctx, request)
	}
	return transport.DecodeError(response.GetError(), err, "plugin."+method)
}

// Enable 显式启用已加载实例
func (c *Client) Enable(ctx context.Context) error { return c.lifecycle(ctx, "enable") }

// Disable 显式禁用已启用实例
func (c *Client) Disable(ctx context.Context) error { return c.lifecycle(ctx, "disable") }

// Unload 调用插件卸载并无条件回收实例，卸载错误不会跳过清理
func (c *Client) Unload(ctx context.Context) error {
	err := c.lifecycle(ctx, "unload")
	_ = c.Close()
	return err
}

// Close 停止反向服务并等待子进程及 Broker 服务实际退出，可重复调用
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.server != nil {
			c.server.Stop()
		}
		c.process.Kill()
		if c.serverDone != nil {
			<-c.serverDone
		}
	})
	return nil
}

// Exited 返回子进程是否已经确认退出
func (c *Client) Exited() bool { return c.process.Exited() }

// ConfigDefaults 读取插件业务默认配置并保留显式空字节
func (c *Client) ConfigDefaults(ctx context.Context) ([]byte, error) {
	response, err := c.endpoint.Control.ConfigDefaults(ctx, &emptypb.Empty{})
	if err := transport.DecodeError(response.GetError(), err, "plugin.config_defaults"); err != nil {
		return nil, err
	}
	if response.GetData() == nil {
		return nil, nil
	}
	return append([]byte{}, response.Data.Value...), nil
}

// ConfigValidate 仅校验候选配置，不改变生效配置
func (c *Client) ConfigValidate(ctx context.Context, data []byte) error {
	request := &wire.ConfigCandidateRequest{}
	if data != nil {
		request.Candidate = &wire.BytesValue{Value: data}
	}
	response, err := c.endpoint.Control.ConfigValidate(ctx, request)
	return transport.DecodeError(response.GetError(), err, "plugin.config_validate")
}

// ConfigApply 原子应用插件业务候选配置
func (c *Client) ConfigApply(ctx context.Context, data []byte) error {
	request := &wire.ConfigCandidateRequest{}
	if data != nil {
		request.Candidate = &wire.BytesValue{Value: data}
	}
	response, err := c.endpoint.Control.ConfigApply(ctx, request)
	return transport.DecodeError(response.GetError(), err, "plugin.config_apply")
}
