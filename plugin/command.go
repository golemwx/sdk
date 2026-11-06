package plugin

import (
	"context"
	"strings"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
)

func (r *runtime) commandSet() (*command.Set, error) {
	r.commandsOnce.Do(func() {
		provider, ok := r.implementation.(CommandProvider)
		if !ok {
			return
		}
		commands, err := readCommands(provider)
		if err != nil {
			r.commandsErr = err
			return
		}
		if commands == nil {
			r.commandsErr = sdk.NewError(
				sdk.CategoryInvalidArgument,
				"plugin.describe",
				"Commands() 返回了空命令声明",
			)
			return
		}
		r.commands, r.commandsErr = command.NewSet(commands)
	})
	return r.commands, r.commandsErr
}

func readCommands(provider CommandProvider) (commands any, err error) {
	defer func() {
		if recover() != nil {
			err = sdk.NewError(sdk.CategoryInternal, "plugin.describe", "命令声明发生 panic")
		}
	}()
	return provider.Commands(), nil
}

func (r *runtime) commandReply(invocation command.Invocation) (command.Reply, error) {
	r.mu.Lock()
	host := r.host
	r.mu.Unlock()
	if host == nil || host.Messages == nil {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "宿主消息能力不可用")
	}
	if invocation.Message == nil {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "命令缺少原始消息")
	}
	target := invocation.Message.Sender.Username
	if invocation.Message.Sender.Kind == contact.KindSelf || target == "" {
		target = invocation.Message.Receiver.Username
	}
	if target == "" {
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.command", "命令消息缺少回复目标")
	}
	return &commandReply{messages: host.Messages, target: target}, nil
}

type commandReply struct {
	messages message.Client
	target   string
}

func (r *commandReply) Text(ctx context.Context, text string) error {
	if ctx == nil {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "回复上下文不能为空")
	}
	if strings.TrimSpace(text) == "" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.command", "回复内容不能为空")
	}
	result, err := r.messages.SendText(ctx, r.target, text, nil)
	if err != nil {
		return err
	}
	if result == nil {
		return sdk.NewError(sdk.CategoryInternal, "plugin.command", "宿主没有返回发送结果")
	}
	return nil
}
