package plugin

import (
	"context"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
)

// businessServer 直接在 gRPC handler 内执行用户代码，避免取消后另起无界协程
type businessServer struct {
	wire.UnimplementedPluginBusinessServiceServer // 保持新方法的前向兼容

	state *controlServer // 共享实例准入、真实执行记录及控制连接
}

// HandleEvent 转换事件并按普通业务额度执行
func (b *businessServer) HandleEvent(ctx context.Context, request *wire.EventRequest) (*wire.EventResponse, error) {
	response := &wire.EventResponse{}
	err := protect("plugin.event", func() error {
		ctx, err := businessContext(ctx, request.GetInvocationId(), request.GetInvocationToken())
		if err != nil {
			return err
		}
		handler, ok := b.state.implementation.(EventHandler)
		if !ok {
			return sdk.NewError(sdk.CategoryUnimplemented, "plugin.event", "event handler is not implemented")
		}
		value, err := wire.FromWireEvent(request.GetEvent())
		if err != nil {
			return err
		}
		if value == nil {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.event", "missing event")
		}
		if err := b.state.beginExecution(ctx, request.InvocationId, executionBusiness); err != nil {
			return err
		}
		defer b.state.finishExecution(request.InvocationId)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		result, err := handler.OnEvent(ctx, *value)
		response.Result = &wire.EventResult{Consumed: result.Consumed}
		return err
	})
	response.Error = transport.EncodeError(err, "plugin.event")
	return response, nil
}

// HandleCommand 与事件共用普通业务额度，完整保留部分结果与业务错误
func (b *businessServer) HandleCommand(ctx context.Context, request *wire.CommandRequest) (*wire.CommandResponse, error) {
	response := &wire.CommandResponse{}
	err := protect("plugin.command", func() error {
		ctx, err := businessContext(ctx, request.GetInvocationId(), request.GetInvocationToken())
		if err != nil {
			return err
		}
		handler, ok := b.state.implementation.(CommandHandler)
		if !ok {
			return sdk.NewError(sdk.CategoryUnimplemented, "plugin.command", "command handler is not implemented")
		}
		value, err := wire.FromWireCommand(request.GetCommand())
		if err != nil {
			return err
		}
		if value == nil {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "missing command")
		}
		if err := b.state.beginExecution(ctx, request.InvocationId, executionBusiness); err != nil {
			return err
		}
		defer b.state.finishExecution(request.InvocationId)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		result, primary := handler.OnCommand(ctx, *value)
		response.Result, err = wire.ToWireCommandResult(result)
		return firstError(primary, err)
	})
	response.Error = transport.EncodeError(err, "plugin.command")
	return response, nil
}

// HandleCapability 使用独立能力额度，满额立即拒绝，不排普通事件队列
func (b *businessServer) HandleCapability(ctx context.Context, request *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	response := &wire.CapabilityResponse{}
	err := protect("plugin.capability", func() error {
		ctx, err := businessContext(ctx, request.GetInvocationId(), request.GetInvocationToken())
		if err != nil {
			return err
		}
		handler, ok := b.state.implementation.(CapabilityHandler)
		if !ok {
			return sdk.NewError(sdk.CategoryUnimplemented, "plugin.capability", "capability handler is not implemented")
		}
		if request.GetRequest() == nil {
			return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.capability", "missing request")
		}
		if len(request.GetRequest().GetPayload().GetValue()) > transport.MaxInlinePayloadSize {
			return sdk.NewError(sdk.CategoryResourceExhausted, "plugin.capability", "inline payload is too large")
		}
		if err := b.state.beginExecution(ctx, request.InvocationId, executionCapability); err != nil {
			return err
		}
		defer b.state.finishExecution(request.InvocationId)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		result, primary := handler.OnCall(ctx, wire.FromWireCallRequest(request.Request))
		if len(result.Payload) > transport.MaxInlinePayloadSize {
			return firstError(primary, sdk.NewError(sdk.CategoryResourceExhausted, "plugin.capability", "inline result is too large"))
		}
		response.Result, err = wire.ToWireCallResult(result)
		return firstError(primary, err)
	})
	response.Error = transport.EncodeError(err, "plugin.capability")
	return response, nil
}
