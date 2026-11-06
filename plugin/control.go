package plugin

import (
	"context"
	"sync"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
	hplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/protobuf/types/known/emptypb"
)

type controlServer struct {
	wire.UnimplementedPluginControlServiceServer

	runtime *runtime
	broker  *hplugin.GRPCBroker

	mu          sync.Mutex
	initialized bool
	hostState   *remoteHostState
	watchCancel context.CancelFunc
}

func newControlServer(runtime *runtime) *controlServer {
	return &controlServer{runtime: runtime}
}

func (c *controlServer) Describe(ctx context.Context, _ *emptypb.Empty) (*wire.DescribeResponse, error) {
	descriptor, err := c.runtime.Describe(ctx)
	response := &wire.DescribeResponse{Error: transport.EncodeError(err, "plugin.describe")}
	if err != nil || descriptor == nil {
		return response, nil
	}
	encoded, err := toWireDescriptor(*descriptor)
	if err != nil {
		response.Error = transport.EncodeError(err, "plugin.describe")
		return response, nil
	}
	response.Descriptor_ = encoded
	return response, nil
}

func (c *controlServer) Initialize(ctx context.Context, request *wire.InitializeRequest) (*wire.OperationResponse, error) {
	if err := contextError(ctx, "plugin.initialize"); err != nil {
		return operationResponse(err, "plugin.initialize"), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized {
		return operationResponse(sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.initialize", "plugin instance is already initialized"), "plugin.initialize"), nil
	}
	if request == nil || request.GetBrokerId() == 0 || c.broker == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, "plugin.initialize", "host broker is invalid"), "plugin.initialize"), nil
	}
	dialOptions := append(transport.DialOptions(), grpc.WithIdleTimeout(0))
	conn, err := c.broker.DialWithOptions(request.GetBrokerId(), dialOptions...)
	if err != nil {
		return operationResponse(sdk.NewError(sdk.CategoryUnavailable, "plugin.initialize", "host connection failed"), "plugin.initialize"), nil
	}
	hostState := newRemoteHostState(conn)
	if err := waitForHostConnection(ctx, conn); err != nil {
		hostState.invalidate(sdk.NewError(sdk.CategoryUnavailable, "plugin.initialize", "host connection is unavailable"))
		return operationResponse(err, "plugin.initialize"), nil
	}
	host := newRemoteHost(hostState)
	err = c.runtime.Initialize(ctx, InitializeOptions{
		Host:    host,
		Config:  wire.FromWireBytes(request.GetConfig()),
		DataDir: request.GetDataRoot(),
	})
	if err != nil {
		hostState.invalidate(sdk.NewError(sdk.CategoryUnavailable, "plugin.initialize", "plugin initialization failed"))
		return operationResponse(err, "plugin.initialize"), nil
	}
	watchCtx, watchCancel := context.WithCancel(context.WithoutCancel(ctx))
	c.hostState = hostState
	c.watchCancel = watchCancel
	c.initialized = true
	go c.watchHostConnection(watchCtx, conn, hostState)
	return operationResponse(nil, "plugin.initialize"), nil
}

func (c *controlServer) Load(ctx context.Context, _ *emptypb.Empty) (*wire.OperationResponse, error) {
	return operationResponse(c.runtime.Load(ctx), "plugin.load"), nil
}

func (c *controlServer) Start(ctx context.Context, _ *emptypb.Empty) (*wire.OperationResponse, error) {
	return operationResponse(c.runtime.Start(ctx), "plugin.start"), nil
}

func (c *controlServer) Stop(ctx context.Context, _ *emptypb.Empty) (*wire.OperationResponse, error) {
	return operationResponse(c.runtime.Stop(ctx), "plugin.stop"), nil
}

func (c *controlServer) Unload(ctx context.Context, _ *emptypb.Empty) (*wire.OperationResponse, error) {
	err := c.runtime.Unload(ctx)
	c.closeHost()
	return operationResponse(err, "plugin.unload"), nil
}

func (c *controlServer) ValidateConfig(ctx context.Context, request *wire.ValidateConfigRequest) (*wire.OperationResponse, error) {
	var config []byte
	if request != nil {
		config = wire.FromWireBytes(request.GetCandidate())
	}
	return operationResponse(c.runtime.ValidateConfig(ctx, config), "plugin.config.validate"), nil
}

func (c *controlServer) SetConfig(ctx context.Context, request *wire.SetConfigRequest) (*wire.OperationResponse, error) {
	var config []byte
	if request != nil {
		config = wire.FromWireBytes(request.GetConfig())
	}
	return operationResponse(c.runtime.SetConfig(ctx, config), "plugin.config.set"), nil
}

func (c *controlServer) closeHost() {
	c.mu.Lock()
	hostState := c.hostState
	cancel := c.watchCancel
	c.hostState = nil
	c.watchCancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if hostState != nil {
		hostState.invalidate(sdk.NewError(sdk.CategoryUnavailable, "plugin.host", "plugin host connection is closed"))
	}
}

func (c *controlServer) watchHostConnection(ctx context.Context, conn *grpc.ClientConn, state *remoteHostState) {
	for {
		current := conn.GetState()
		if current == connectivity.Idle || current == connectivity.Shutdown || current == connectivity.TransientFailure {
			state.invalidate(sdk.NewError(sdk.CategoryUnavailable, "plugin.host", "plugin host connection was lost"))
			c.runtime.hostLost(sdk.NewError(sdk.CategoryUnavailable, "plugin.host", "plugin host connection was lost"))
			return
		}
		if !conn.WaitForStateChange(ctx, current) {
			return
		}
	}
}

func waitForHostConnection(ctx context.Context, conn *grpc.ClientConn) error {
	conn.Connect()
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return nil
		case connectivity.TransientFailure, connectivity.Shutdown:
			return sdk.NewError(sdk.CategoryUnavailable, "plugin.initialize", "host connection is unavailable")
		}
		if !conn.WaitForStateChange(ctx, state) {
			return contextError(ctx, "plugin.initialize")
		}
	}
}

func operationResponse(err error, operation string) *wire.OperationResponse {
	return &wire.OperationResponse{Error: transport.EncodeError(err, operation)}
}
