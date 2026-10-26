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
			wire.RegisterPluginBusinessServiceServer(server, &wire.UnimplementedPluginBusinessServiceServer{})
			return nil
		}),
		GRPCServer: transport.Server,
		Logger:     hclog.NewNullLogger(),
	})
}

// controlServer 管理插件控制面，业务 handler 的额度和执行跟踪由后续 wrapper 接入
type controlServer struct {
	// UnimplementedPluginControlServiceServer 为尚未接入的方法返回明确的 Unimplemented
	wire.UnimplementedPluginControlServiceServer
	// implementation 插件作者提供的可选接口实现
	implementation Plugin
	// broker 当前插件实例的唯一反向 Broker
	broker *hplugin.GRPCBroker
	// mu 串行化初始化和生命周期状态变更
	mu sync.Mutex
	// describeOnce 确保插件固定声明只读取一次
	describeOnce sync.Once
	// descriptor 缓存的不可变传输声明
	descriptor *wire.Descriptor
	// describeError 描述读取或插件 panic 对应的脱敏错误
	describeError error
	// conn 初始化成功建立的唯一反向宿主连接
	conn *grpc.ClientConn
	// host 显式传给 OnLoad 的宿主客户端集合
	host *Host
	// controlToken 当前实例的控制凭据
	controlToken string
	// generation 当前插件实例代次
	generation uint64
	// loaded 插件是否已经完成加载
	loaded bool
	// enabled 插件是否已经启用
	enabled bool
	// unloaded 插件是否已卸载，禁止重用同一实例
	unloaded bool
}

// protect 将插件 panic 转换成脱敏错误，避免破坏控制服务
func protect(operation func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = sdk.NewError(sdk.CategoryInternal, "plugin.control", "plugin handler panicked")
		}
	}()
	return operation()
}

// Describe 返回固定描述副本，避免后续调用修改缓存
func (s *controlServer) Describe(ctx context.Context, _ *emptypb.Empty) (*wire.DescribeResponse, error) {
	s.describeOnce.Do(func() {
		s.describeError = protect(func() error {
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
	err := protect(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.conn != nil || s.unloaded {
			return sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "instance already initialized or unloaded")
		}
		if request.GetBrokerId() == 0 || request.GetControlToken() == "" || request.GetInstanceGeneration() == 0 || !filepath.IsAbs(request.GetDataDir()) {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "invalid initialization context")
		}
		conn, err := s.broker.DialWithOptions(request.BrokerId, transport.DialOptions()...)
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
		s.host = newRemoteHost(conn, request.DataDir)
		s.conn, s.controlToken, s.generation = conn, request.ControlToken, request.InstanceGeneration
		succeeded = true
		return nil
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.initialize")}, nil
}

// lifecycle 在初始化之后串行执行可选生命周期，并保持原调用上下文
func (s *controlServer) lifecycle(ctx context.Context, method string) (*wire.OperationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := protect(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.conn == nil || s.unloaded {
			return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance is not initialized")
		}
		switch method {
		case "plugin.load":
			if s.loaded {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance already loaded")
			}
			if handler, ok := s.implementation.(Loader); ok {
				if err := handler.OnLoad(ctx, s.host); err != nil {
					return err
				}
			}
			s.loaded = true
		case "plugin.enable":
			if !s.loaded || s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance cannot be enabled")
			}
			if handler, ok := s.implementation.(Enabler); ok {
				if err := handler.OnEnable(ctx); err != nil {
					return err
				}
			}
			s.enabled = true
		case "plugin.disable":
			if !s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance is not enabled")
			}
			if handler, ok := s.implementation.(Disabler); ok {
				if err := handler.OnDisable(ctx); err != nil {
					return err
				}
			}
			s.enabled = false
		case "plugin.unload":
			if !s.loaded || s.enabled {
				return sdk.NewError(sdk.CategoryFailedPrecondition, method, "instance cannot be unloaded")
			}
			defer func() { s.unloaded = true; _ = s.conn.Close() }()
			if handler, ok := s.implementation.(Unloader); ok {
				return handler.OnUnload(ctx)
			}
		}
		return nil
	})
	return &wire.OperationResponse{Error: transport.EncodeError(err, method)}, nil
}

// OnLoad 将显式 Host 交给实现了 Loader 的插件
func (s *controlServer) OnLoad(ctx context.Context, _ *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.load")
}

// OnEnable 调用可选启用接口
func (s *controlServer) OnEnable(ctx context.Context, _ *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.enable")
}

// OnDisable 调用可选禁用接口
func (s *controlServer) OnDisable(ctx context.Context, _ *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.disable")
}

// OnUnload 调用可选卸载接口并关闭反向连接
func (s *controlServer) OnUnload(ctx context.Context, _ *wire.LifecycleRequest) (*wire.OperationResponse, error) {
	return s.lifecycle(ctx, "plugin.unload")
}

// Drain 校验实例控制身份，实际执行跟踪接入前不返回空排空成功
func (s *controlServer) Drain(ctx context.Context, request *wire.DrainRequest) (*wire.DrainResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	category := sdk.CategoryUnimplemented
	if s.controlToken == "" || subtle.ConstantTimeCompare([]byte(request.GetControlToken()), []byte(s.controlToken)) != 1 || request.GetInstanceGeneration() != s.generation {
		category = sdk.CategoryPermissionDenied
	}
	return &wire.DrainResponse{Error: wire.ToWireError(sdk.NewError(category, "plugin.drain", "execution tracking is not available"))}, nil
}

// ConfigDefaults 调用可选的业务默认配置接口
func (s *controlServer) ConfigDefaults(ctx context.Context, _ *emptypb.Empty) (*wire.ConfigDefaultsResponse, error) {
	response := &wire.ConfigDefaultsResponse{}
	err := protect(func() error {
		handler, ok := s.implementation.(ConfigDefaulter)
		if !ok {
			return sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_defaults", "configuration defaults are not implemented")
		}
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
	handler, ok := s.implementation.(ConfigValidator)
	if !ok {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_validate", "configuration validation is not implemented"))}, nil
	}
	err := protect(func() error { return handler.ConfigValidate(ctx, configBytes(request.GetCandidate())) })
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.config_validate")}, nil
}

// ConfigApply 调用插件原子配置应用接口
func (s *controlServer) ConfigApply(ctx context.Context, request *wire.ConfigCandidateRequest) (*wire.OperationResponse, error) {
	handler, ok := s.implementation.(ConfigApplier)
	if !ok {
		return &wire.OperationResponse{Error: wire.ToWireError(sdk.NewError(sdk.CategoryUnimplemented, "plugin.config_apply", "configuration application is not implemented"))}, nil
	}
	err := protect(func() error { return handler.ConfigApply(ctx, configBytes(request.GetCandidate())) })
	return &wire.OperationResponse{Error: transport.EncodeError(err, "plugin.config_apply")}, nil
}

// configBytes 复制可选配置载荷并保留 nil 与显式空的区别
func configBytes(value *wire.BytesValue) []byte {
	if value == nil {
		return nil
	}
	return append([]byte{}, value.Value...)
}
