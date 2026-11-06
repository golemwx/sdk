package plugin

import (
	"context"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
)

type businessServer struct {
	wire.UnimplementedPluginBusinessServiceServer

	runtime *runtime
}

func (b *businessServer) HandleEvent(ctx context.Context, request *wire.HandleEventRequest) (*wire.EventResponse, error) {
	response := &wire.EventResponse{}
	if request == nil || request.GetEvent() == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, "plugin.event", "event is missing"), "plugin.event")
		return response, nil
	}
	value, err := wire.FromWireEvent(request.GetEvent())
	if err != nil {
		response.Error = transport.EncodeError(err, "plugin.event")
		return response, nil
	}
	result, err := b.runtime.HandleEvent(ctx, *value)
	response.Result = wire.ToWireEventResult(result)
	response.Error = transport.EncodeError(err, "plugin.event")
	return response, nil
}

func (b *businessServer) HandleCommand(ctx context.Context, request *wire.HandleCommandRequest) (*wire.OperationResponse, error) {
	if request == nil || request.GetCommand() == nil {
		return operationResponse(sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "command is missing"), "plugin.command"), nil
	}
	value, err := wire.FromWireCommandInvocation(request.GetCommand())
	if err != nil {
		return operationResponse(err, "plugin.command"), nil
	}
	return operationResponse(b.runtime.HandleCommand(ctx, *value), "plugin.command"), nil
}

func (b *businessServer) HandleCapability(ctx context.Context, request *wire.HandleCapabilityRequest) (*wire.CapabilityResponse, error) {
	response := &wire.CapabilityResponse{}
	if request == nil || request.GetRequest() == nil {
		response.Error = transport.EncodeError(sdk.NewError(sdk.CategoryInvalidArgument, "plugin.capability", "capability request is missing"), "plugin.capability")
		return response, nil
	}
	value := wire.FromWireCapabilityRequest(request.GetRequest())
	result, err := b.runtime.HandleCapability(ctx, capability.Request{
		Name:        value.Name,
		ContentType: value.ContentType,
		Payload:     append([]byte(nil), value.Payload...),
	})
	response.Result = wire.ToWireCapabilityResult(result)
	response.Error = transport.EncodeError(err, "plugin.capability")
	return response, nil
}
