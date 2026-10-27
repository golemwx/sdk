package plugin

import (
	"context"
	"crypto/subtle"
	"path/filepath"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	"github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Serve 使用 SDK 唯一配对配置运行插件，握手 stdout 由 go-plugin 独占
func Serve(implementation Plugin) {
	state := &controlServer{implementation: implementation}
	hplugin.Serve(&hplugin.ServeConfig{
		HandshakeConfig: transport.Handshake(),
		Plugins: transport.PluginSet(func(broker *hplugin.GRPCBroker, server *grpc.Server) error {
			state.broker = broker
			wire.RegisterPluginControlServiceServer(server, state)
			wire.RegisterPluginBusinessServiceServer(server, &businessServer{state: state})
			return nil
		}),
		GRPCServer: transport.Server,
		Logger:     hclog.NewNullLogger(),
	})
}

// controlServer 管理插件控制面与实例执行记录
type controlServer struct {
	wire.UnimplementedPluginControlServiceServer // 为尚未接入的方法返回明确的 Unimplemented

	implementation Plugin                        // 插件作者提供的可选接口实现
	broker         *hplugin.GRPCBroker           // 当前插件实例的唯一反向 Broker
	mu             sync.Mutex                    // 串行化初始化和生命周期状态变更
	describeOnce   sync.Once                     // 确保插件固定声明只读取一次
	descriptor     *wire.Descriptor              // 缓存的不可变传输声明
	describeError  error                         // 描述读取或插件 panic 对应的脱敏错误
	conn           *grpc.ClientConn              // 初始化成功建立的唯一反向宿主连接
	host           *Host                         // 当前反向连接对应的宿主客户端集合
	binding        *hostBinding                  // Base 引用对应的私有连接代次
	controlToken   string                        // 当前实例的控制凭据
	generation     uint64                        // 当前插件实例代次
	loaded         bool                          // 插件是否已经完成加载
	enabled        bool                          // 插件是否已经启用
	unloaded       bool                          // 插件是否已卸载，禁止重用同一实例
	executionMu    sync.Mutex                    // 保护业务准入和实际执行记录，不被生命周期用户代码持有
	active         map[string]executionKind      // 用户代码尚未实际退出的 wrapper 执行
	accepting      bool                          // 是否允许新的业务执行
	admissionEpoch uint64                        // 防止排空期间迟到的 OnEnable 重新打开准入
	controlContext context.Context               // 不继承单次业务取消的实例控制生命周期
	controlCancel  context.CancelFunc            // 实例失效或卸载时取消独立控制生命周期
	completion     wire.HostControlServiceClient // 唯一反向连接上的受控完成确认客户端
}

// protect 将插件 panic 转换成脱敏错误，避免破坏控制服务
func protect(operation string, handler func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = sdk.NewError(sdk.CategoryInternal, operation, "plugin handler panicked")
		}
	}()
	return handler()
}

// Describe 返回固定描述副本，避免后续调用修改缓存
func (s *controlServer) Describe(ctx context.Context, _ *emptypb.Empty) (*wire.DescribeResponse, error) {
	s.describeOnce.Do(func() {
		s.describeError = protect("plugin.describe", func() error {
			if s.implementation == nil {
				return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.describe", "plugin is nil")
			}
			s.descriptor = wire.ToWireDescriptor(s.implementation.Descriptor())
			return nil
		})
	})
	response := &wire.DescribeResponse{Error: transport.EncodeError(s.describeError, "plugin.describe")}
	if s.descriptor != nil {
		response.Descriptor_ = proto.Clone(s.descriptor).(*wire.Descriptor)
	}
	return response, nil
}

// Initialize 建立唯一的安全反向连接并等待宿主确认
func (s *controlServer) Initialize(ctx context.Context, request *wire.InitializeRequest) (*wire.OperationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := protect("plugin.initialize", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.conn != nil || s.unloaded {
			return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "instance already initialized or unloaded")
		}
		if request.GetBrokerId() == 0 || request.GetControlToken() == "" || request.GetInstanceGeneration() == 0 || !filepath.IsAbs(request.GetDataDir()) {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "invalid initialization context")
		}
		if err := s.validateHandlers(ctx); err != nil {
			return err
		}
		// 反向连接与实例同寿命，禁止因长时间无调用而主动进入 Idle
		dialOptions := append(transport.DialOptions(), grpc.WithIdleTimeout(0))
		conn, err := s.broker.DialWithOptions(request.BrokerId, dialOptions...)
		if err != nil {
			return sdk.NewError(sdk.CategoryUnavailable, "plugin.initialize", "reverse connection failed")
		}
		succeeded := false
		defer func() {
			if !succeeded {
				_ = conn.Close()
			}
		}()
		response, err := wire.NewHostControlServiceClient(conn).ReverseReady(ctx, &wire.ReverseReadyRequest{ControlToken: request.ControlToken})
		if err := transport.DecodeError(response.GetError(), err, "host.reverse_ready"); err != nil {
			return err
		}
		host := newRemoteHost(conn, request.DataDir)
		var binding *hostBinding
		if binder, ok := s.implementation.(hostBinder); ok {
			binding = &hostBinding{}
			if err := binder.bindHost(host, binding); err != nil {
				return err
			}
			defer func() {
				if !succeeded {
					binder.unbindHost(binding)
				}
			}()
		}
		// Initialize 没有用户 invocation，脱离该单次 RPC 仅用于实例控制及后台周期
		controlContext, controlCancel := context.WithCancel(context.WithoutCancel(ctx))
		defer func() {
			if !succeeded {
				controlCancel()
			}
		}()
		host.startTasks(controlContext)
		host.stopTasks()
		s.executionMu.Lock()
		s.host, s.binding = host, binding
		s.conn, s.controlToken, s.generation = conn, request.ControlToken, request.InstanceGeneration
		s.controlContext, s.controlCancel = controlContext, controlCancel
		s.completion = wire.NewHostControlServiceClient(conn)
		s.executionMu.Unlock()
		go s.watchConnection(conn, controlContext, controlCancel, binding)
		succeeded = true
		return nil
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.initialize")}, nil
}

// lifecycle 在初始化之后串行执行可选生命周期，并保持原调用上下文
func (s *controlServer) lifecycle(ctx context.Context, method, id string) (*wire.OperationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if method == "plugin.unload" && s.unloaded {
			s.invalidateConnection(s.conn, s.controlCancel, s.binding)
			_ = s.conn.Close()
		}
	}()
	err := protect(method, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.conn == nil || s.unloaded {
			return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance is not initialized")
		}
		var err error
		ctx, err = lifecycleContext(ctx, id)
		if err != nil {
			return err
		}
		if err := s.beginExecution(ctx, id, executionControl); err != nil {
			return err
		}
		defer s.finishExecution(id)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		switch method {
		case "plugin.load":
			if s.loaded {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance already loaded")
			}
			if handler, ok := s.implementation.(Loader); ok {
				if err := handler.OnLoad(ctx); err != nil {
					return err
				}
			}
			s.loaded = true
		case "plugin.enable":
			if !s.loaded || s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance cannot be enabled")
			}
			s.executionMu.Lock()
			epoch := s.admissionEpoch
			cancelTasks := s.host.startTasks(s.controlContext)
			s.executionMu.Unlock()
			succeeded := false
			// 启用回调忽略取消时，也立即撤销尚未提交的后台周期
			stopCancel := context.AfterFunc(ctx, func() {
				s.executionMu.Lock()
				defer s.executionMu.Unlock()
				if !succeeded {
					cancelTasks()
				}
			})
			defer stopCancel()
			defer func() {
				if !succeeded {
					cancelTasks()
				}
			}()
			if handler, ok := s.implementation.(Enabler); ok {
				if err := handler.OnEnable(ctx); err != nil {
					return err
				}
			}
			s.executionMu.Lock()
			defer s.executionMu.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			if s.admissionEpoch != epoch || s.controlContext.Err() != nil {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "enable was canceled or drained")
			}
			s.enabled = true
			s.accepting = true
			succeeded = true
		case "plugin.disable":
			if !s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance is not enabled")
			}
			s.stopAdmission()
			s.enabled = false
			if handler, ok := s.implementation.(Disabler); ok {
				if err := handler.OnDisable(ctx); err != nil {
					return err
				}
			}
		case "plugin.unload":
			if !s.loaded || s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance cannot be unloaded")
			}
			s.stopAdmission()
			defer func() { s.unloaded = true }()
			if handler, ok := s.implementation.(Unloader); ok {
				return handler.OnUnload(ctx)
			}
		}
		return nil
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, method)}, nil
}

// lifecycleContext 仅接受安全正向连接 metadata 中与请求标识一致的宿主单次凭据
func lifecycleContext(ctx context.Context, id string) (context.Context, error) {
	ids := metadata.ValueFromIncomingContext(ctx, transport.LifecycleInvocationIDHeader)
	tokens := metadata.ValueFromIncomingContext(ctx, transport.LifecycleInvocationTokenHeader)
	if id == "" || len(ids) != 1 || len(tokens) != 1 || tokens[0] == "" || subtle.ConstantTimeCompare([]byte(id), []byte(ids[0])) != 1 {
		return nil, sdk.NewError(sdk.CategoryPermissionDenied, "plugin.lifecycle", "invalid lifecycle invocation identity")
	}
	return transport.WithInvocation(ctx, id, tokens[0]), nil
}

// OnLoad 调用可选加载接口，Base 已在 Initialize 中自动绑定 Host
func (s *controlServer) OnLoad(ctx context.Context, request *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.load", request.GetInvocationId())
}

// OnEnable 调用可选启用接口
func (s *controlServer) OnEnable(ctx context.Context, request *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.enable", request.GetInvocationId())
}

// OnDisable 调用可选禁用接口
func (s *controlServer) OnDisable(ctx context.Context, request *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.disable", request.GetInvocationId())
}

// OnUnload 调用可选卸载接口并关闭反向连接
func (s *controlServer) OnUnload(ctx context.Context, request *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.unload", request.GetInvocationId())
}

// Drain 原子关闭业务准入并查询真实执行记录，控制操作不等待用户 handler
func (s *controlServer) Drain(ctx context.Context, request *wire.DrainRequest) (*wire.DrainResponse, error) {
	s.executionMu.Lock()
	valid := s.controlToken != "" && subtle.ConstantTimeCompare([]byte(request.GetControlToken()), []byte(s.controlToken)) == 1 && request.GetInstanceGeneration() == s.generation
	s.executionMu.Unlock()
	if !valid {
		return &wire.DrainResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryPermissionDenied, "plugin.drain", "invalid instance control identity"))}, nil
	}
	if err := ctx.Err(); err != nil {
		return &wire.DrainResponse{Error: transport.EncodeError(err, "plugin.drain")}, nil
	}
	s.stopAdmission()
	return &wire.DrainResponse{ActiveInvocationIds: s.activeExecutions()}, nil
}

// validateHandlers 拒绝声明业务入口却没有实现对应接口的插件
func (s *controlServer) validateHandlers(ctx context.Context) error {
	response, _ := s.Describe(ctx, &emptypb.Empty{})
	if err := transport.DecodeError(response.GetError(), nil, "plugin.describe"); err != nil {
		return err
	}
	d := response.GetDescriptor_()
	_, events := s.implementation.(EventHandler)
	_, commands := s.implementation.(CommandHandler)
	_, capabilities := s.implementation.(CapabilityHandler)
	if (len(d.GetSubscriptions()) > 0 && !events) || (len(d.GetCommands()) > 0 && !commands) || (len(d.GetCapabilities()) > 0 && !capabilities) {
		return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "declared business handler is missing")
	}
	return nil
}

// watchConnection 在所属宿主连接失效时撤销后台周期、业务准入和 Base 绑定
func (s *controlServer) watchConnection(conn *grpc.ClientConn, controlContext context.Context, controlCancel context.CancelFunc, binding *hostBinding) {
	for {
		state := conn.GetState()
		// 已完成 ReverseReady 且关闭闲置超时后，Idle 也说明底层连接已断开
		if state == connectivity.Idle || state == connectivity.Shutdown || state == connectivity.TransientFailure {
			s.invalidateConnection(conn, controlCancel, binding)
			_ = conn.Close()
			return
		}
		if !conn.WaitForStateChange(controlContext, state) {
			return
		}
	}
}

// invalidateConnection 仅清理由同一连接和私有代次建立的实例状态
func (s *controlServer) invalidateConnection(conn *grpc.ClientConn, controlCancel context.CancelFunc, binding *hostBinding) {
	s.executionMu.Lock()
	if s.conn != conn || s.binding != binding {
		s.executionMu.Unlock()
		return
	}
	s.accepting = false
	s.admissionEpoch++
	host := s.host
	s.executionMu.Unlock()
	if host != nil {
		host.stopTasks()
	}
	if controlCancel != nil {
		controlCancel()
	}
	if binder, ok := s.implementation.(hostBinder); ok {
		binder.unbindHost(binding)
	}
}

// ConfigDefaults 调用可选的业务默认配置接口
func (s *controlServer) ConfigDefaults(ctx context.Context, _ *emptypb.Empty) (*wire.ConfigDefaultsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	response := &wire.ConfigDefaultsResponse{}
	err := protect("plugin.config_defaults", func() error {
		handler, ok := s.implementation.(ConfigDefaulter)
		if !ok {
			return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_defaults", "configuration defaults are not implemented")
		}
		ids := metadata.ValueFromIncomingContext(ctx, transport.ControlInvocationHeader)
		if len(ids) != 1 {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config_defaults", "missing invocation ID")
		}
		if err := s.beginExecution(ctx, ids[0], executionControl); err != nil {
			return err
		}
		defer s.finishExecution(ids[0])
		data := handler.ConfigDefaults()
		if data != nil {
			response.Data = &wire.BytesValue{Value: append([]byte{}, data...)}
		}
		return nil
	})
	response.Error = transport.EncodeError(err, "plugin.config_defaults")
	return response, nil
}

// ConfigValidate 调用插件纯校验接口，保留候选值存在性
func (s *controlServer) ConfigValidate(ctx context.Context, request *wire.ConfigCandidateRequest) (*wire.OperationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handler, ok := s.implementation.(ConfigValidator)
	if !ok {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_validate", "configuration validation is not implemented"))}, nil
	}
	err := protect("plugin.config_validate", func() error {
		if err := s.beginExecution(ctx, request.GetInvocationId(), executionControl); err != nil {
			return err
		}
		defer s.finishExecution(request.InvocationId)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		return handler.ConfigValidate(ctx, configBytes(request.GetCandidate()))
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.config_validate")}, nil
}

// ConfigApply 调用插件原子配置应用接口
func (s *controlServer) ConfigApply(ctx context.Context, request *wire.ConfigCandidateRequest) (*wire.OperationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	handler, ok := s.implementation.(ConfigApplier)
	if !ok {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_apply", "configuration application is not implemented"))}, nil
	}
	err := protect("plugin.config_apply", func() error {
		if err := s.beginExecution(ctx, request.GetInvocationId(), executionControl); err != nil {
			return err
		}
		defer s.finishExecution(request.InvocationId)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		return handler.ConfigApply(ctx, configBytes(request.GetCandidate()))
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.config_apply")}, nil
}

// configBytes 复制可选配置载荷并保留 nil 与显式空的区别
func configBytes(value *wire.BytesValue) []byte {
	if value == nil {
		return nil
	}
	return append([]byte{}, value.Value...)
}
