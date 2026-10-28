package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/capability"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
	"github.com/golemwx/sdk/internal/transport"
	"github.com/golemwx/sdk/internal/wire"
)

// newInvocationID 为宿主控制调用分配不可复用的随机执行标识
func newInvocationID() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// newInvocation 为 SDK 控制操作同时签发不可预测的标识和凭据
func newInvocation() (Invocation, error) {
	var value [64]byte
	if _, err := rand.Read(value[:]); err != nil {
		return Invocation{}, err
	}
	return Invocation{ID: hex.EncodeToString(value[:32]), Token: hex.EncodeToString(value[32:])}, nil
}

// validateInvocation 宿主必须显式提供自身签发的唯一执行标识和 token
func validateInvocation(invocation Invocation) error {
	if invocation.ID == "" || invocation.Token == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.execute", "missing host invocation identity")
	}
	return nil
}

// HandleEvent 投递一次事件，返回或超时仅结束等待，不代表插件 handler 已退出
func (c *Client) HandleEvent(ctx context.Context, invocation Invocation, value event.Event) (event.EventResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return event.EventResult{}, err
	}
	request, err := wire.ToWireEvent(&value)
	if err != nil {
		return event.EventResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	revoke, err := c.control.bindInvocation(ctx, invocation)
	if err != nil {
		return event.EventResult{}, err
	}
	defer revoke()
	response, err := c.endpoint.Business.HandleEvent(ctx, &wire.EventRequest{InvocationId: invocation.ID, InvocationToken: invocation.Token, Event: request})
	return event.EventResult{Consumed: response.GetResult().GetConsumed()}, transport.ResultError(response.GetResult() != nil, response.GetError(), err, "plugin.event")
}

// HandleCommand 投递宿主已校验的命令，保持父取消和更早的截止时间
func (c *Client) HandleCommand(ctx context.Context, invocation Invocation, value command.Command) (command.CommandResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return command.CommandResult{}, err
	}
	request, err := wire.ToWireCommand(&value)
	if err != nil {
		return command.CommandResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	revoke, err := c.control.bindInvocation(ctx, invocation)
	if err != nil {
		return command.CommandResult{}, err
	}
	defer revoke()
	response, err := c.endpoint.Business.HandleCommand(ctx, &wire.CommandRequest{InvocationId: invocation.ID, InvocationToken: invocation.Token, Command: request})
	result, conversion := wire.FromWireCommandResult(response.GetResult())
	if err := transport.ResultError(response.GetResult() != nil, response.GetError(), err, "plugin.command"); err != nil {
		return result, err
	}
	return result, conversion
}

// HandleCapability 投递能力调用，真实完成由控制通知或 Drain 单独确认
func (c *Client) HandleCapability(ctx context.Context, invocation Invocation, value capability.CallRequest) (capability.CallResult, error) {
	if err := validateInvocation(invocation); err != nil {
		return capability.CallResult{}, err
	}
	if len(value.Payload) > MaxInlinePayloadSize {
		return capability.CallResult{}, sdk.NewError(sdk.CategoryResourceExhausted, "plugin.capability", "inline payload is too large")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	revoke, err := c.control.bindInvocation(ctx, invocation)
	if err != nil {
		return capability.CallResult{}, err
	}
	defer revoke()
	response, err := c.endpoint.Business.HandleCapability(ctx, &wire.CapabilityRequest{InvocationId: invocation.ID, InvocationToken: invocation.Token, Request: wire.ToWireCallRequest(value)})
	result, conversion := wire.FromWireCallResult(response.GetResult())
	if err := transport.ResultError(response.GetResult() != nil, response.GetError(), err, "plugin.capability"); err != nil {
		return result, err
	}
	return result, conversion
}

// Drain 关闭插件业务准入并返回 wrapper 中仍未实际结束的执行，不把超时视为排空
func (c *Client) Drain(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	response, err := c.endpoint.Control.Drain(ctx, &wire.DrainRequest{ControlToken: c.control.token, InstanceGeneration: c.control.generation})
	if err := transport.DecodeError(response.GetError(), err, "plugin.drain"); err != nil {
		return nil, err
	}
	return append([]string{}, response.GetActiveInvocationIds()...), nil
}
