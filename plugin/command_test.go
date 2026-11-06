package plugin

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
)

type runtimeCommandRecorder struct {
	mu        sync.Mutex
	echoCalls []runtimeEchoCommand
	runs      int
	ownerRuns int
}

type runtimeCommandPlugin struct {
	*Base
	recorder *runtimeCommandRecorder
}

func (*runtimeCommandPlugin) Metadata() Metadata {
	return Metadata{ID: "command-fixture", Name: "命令测试"}
}

func (*runtimeCommandPlugin) Commands() any {
	return &runtimeCommands{}
}

type runtimeCommands struct {
	Echo  runtimeEchoCommand  `cmd:"" name:"echo" help:"回复文本"`
	Owner runtimeOwnerCommand `cmd:"" name:"owner" scope:"owner" help:"管理员命令"`
}

type runtimeEchoCommand struct {
	Text    string `arg:"" name:"text" help:"文本"`
	Target  string `short:"t" name:"target" required:"" help:"目标"`
	Enabled bool   `short:"e" name:"enabled" default:"true" help:"启用"`
}

func (c *runtimeEchoCommand) Run(
	ctx context.Context,
	plugin *runtimeCommandPlugin,
	reply command.Reply,
) error {
	plugin.recorder.mu.Lock()
	plugin.recorder.echoCalls = append(plugin.recorder.echoCalls, *c)
	plugin.recorder.runs++
	plugin.recorder.mu.Unlock()
	return reply.Text(ctx, "echo:"+c.Text)
}

type runtimeOwnerCommand struct{}

func (*runtimeOwnerCommand) Run(
	ctx context.Context,
	plugin *runtimeCommandPlugin,
	reply command.Reply,
) error {
	plugin.recorder.mu.Lock()
	plugin.recorder.ownerRuns++
	plugin.recorder.mu.Unlock()
	return reply.Text(ctx, "owner")
}

type runtimeCommandMessages struct {
	mu      sync.Mutex
	targets []string
	texts   []string
}

func (m *runtimeCommandMessages) SendText(
	_ context.Context,
	target, text string,
	_ []string,
) (*message.SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets = append(m.targets, target)
	m.texts = append(m.texts, text)
	return &message.SendResult{Outcome: sdk.OutcomeSucceeded}, nil
}

func (*runtimeCommandMessages) SendApp(context.Context, string, message.App) (*message.SendResult, error) {
	return nil, nil
}

func (*runtimeCommandMessages) SendMedia(context.Context, string, message.Upload) (*message.SendResult, error) {
	return nil, nil
}

func (*runtimeCommandMessages) Forward(context.Context, string, message.Message) (*message.SendResult, error) {
	return nil, nil
}

func (*runtimeCommandMessages) Revoke(context.Context, message.SendResult) error {
	return nil
}

func (*runtimeCommandMessages) Download(context.Context, message.Message) (io.ReadCloser, error) {
	return nil, nil
}

func newCommandRuntime(t *testing.T) (*runtime, *runtimeCommandRecorder, *runtimeCommandMessages) {
	t.Helper()
	recorder := &runtimeCommandRecorder{}
	plugin := &runtimeCommandPlugin{Base: NewBase(), recorder: recorder}
	instance, err := newRuntime(plugin)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	messages := &runtimeCommandMessages{}
	host := NewHost(nil, messages, nil, nil, nil, nil, nil)
	if err := instance.Initialize(t.Context(), InitializeOptions{Host: host, DataDir: t.TempDir()}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := instance.Unload(context.Background()); err != nil {
			t.Errorf("Unload() error = %v", err)
		}
	})
	return instance, recorder, messages
}

func commandMessage() *message.Message {
	return &message.Message{
		Sender:   contact.Contact{Username: "room", Kind: contact.KindRoom},
		Receiver: contact.Contact{Username: "bot", Kind: contact.KindSelf},
	}
}

// TestRuntimeDescribeGeneratesCommandSchema 验证 Describe 使用结构体命令声明。
func TestRuntimeDescribeGeneratesCommandSchema(t *testing.T) {
	recorder := &runtimeCommandRecorder{}
	instance, err := newRuntime(&runtimeCommandPlugin{Base: NewBase(), recorder: recorder})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	descriptor, err := instance.Describe(t.Context())
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if len(descriptor.Commands.Roots) != 2 {
		t.Fatalf("command roots = %+v", descriptor.Commands.Roots)
	}
	var ownerScope command.Scope
	for _, root := range descriptor.Commands.Roots {
		if root.Name == "owner" {
			ownerScope = root.Scope
		}
	}
	if ownerScope != command.ScopeOwner {
		t.Fatalf("owner command scope = %v, want owner", ownerScope)
	}
}

// TestRuntimeHandleCommandBindsAndReplies 验证 Kong 绑定、Run 注入和 Reply。
func TestRuntimeHandleCommandBindsAndReplies(t *testing.T) {
	instance, recorder, messages := newCommandRuntime(t)
	err := instance.HandleCommand(t.Context(), command.Invocation{
		Tokens:  []string{"echo", "hello", "-t", "room", "--enabled=false"},
		Message: commandMessage(),
	})
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}
	if len(recorder.echoCalls) != 1 {
		t.Fatalf("Run count = %d, want 1", len(recorder.echoCalls))
	}
	if got := recorder.echoCalls[0]; got.Text != "hello" || got.Target != "room" || got.Enabled {
		t.Fatalf("bound command = %+v", got)
	}
	if !slices.Equal(messages.targets, []string{"room"}) {
		t.Fatalf("reply targets = %v", messages.targets)
	}
	if !slices.Equal(messages.texts, []string{"echo:hello"}) {
		t.Fatalf("reply texts = %v", messages.texts)
	}
}

// TestRuntimeRawCommandUsesSharedLexer 验证缺少 Tokens 时使用统一 lexer。
func TestRuntimeRawCommandUsesSharedLexer(t *testing.T) {
	instance, recorder, messages := newCommandRuntime(t)
	err := instance.HandleCommand(t.Context(), command.Invocation{
		Raw:     `/echo "hello world" -t room`,
		Message: commandMessage(),
	})
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}
	if len(recorder.echoCalls) != 1 || recorder.echoCalls[0].Text != "hello world" {
		t.Fatalf("raw command binding = %+v", recorder.echoCalls)
	}
	if !slices.Equal(messages.texts, []string{"echo:hello world"}) {
		t.Fatalf("reply texts = %v", messages.texts)
	}
}

// TestRuntimeHelpDoesNotRunBusiness 验证 help 只回复帮助，不调用 Run 或退出进程。
func TestRuntimeHelpDoesNotRunBusiness(t *testing.T) {
	instance, recorder, messages := newCommandRuntime(t)
	err := instance.HandleCommand(t.Context(), command.Invocation{
		Tokens:  []string{"echo", "--help"},
		Message: commandMessage(),
	})
	if err != nil {
		t.Fatalf("HandleCommand(--help) error = %v", err)
	}
	if recorder.runs != 0 {
		t.Fatalf("help triggered %d business calls", recorder.runs)
	}
	if len(messages.texts) != 1 {
		t.Fatalf("help reply count = %d, want 1", len(messages.texts))
	}
	for _, want := range []string{"golem echo", "--target", "目标"} {
		if !strings.Contains(messages.texts[0], want) {
			t.Fatalf("help reply %q does not contain %q", messages.texts[0], want)
		}
	}
}

// TestRuntimeOwnerCommandRejectedBeforeRun 验证 SDK 侧 owner 二次校验。
func TestRuntimeOwnerCommandRejectedBeforeRun(t *testing.T) {
	instance, recorder, _ := newCommandRuntime(t)
	err := instance.HandleCommand(t.Context(), command.Invocation{
		Tokens:  []string{"owner"},
		Message: commandMessage(),
		Owner:   false,
	})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryPermissionDenied {
		t.Fatalf("HandleCommand(owner) error = %v, want PermissionDenied", err)
	}
	if recorder.ownerRuns != 0 {
		t.Fatal("non-owner command reached Run")
	}
}

// TestRuntimeCommandValuesStayIsolated 验证两次命令调用使用独立值。
func TestRuntimeCommandValuesStayIsolated(t *testing.T) {
	instance, recorder, _ := newCommandRuntime(t)
	for _, invocation := range []command.Invocation{
		{Tokens: []string{"echo", "first", "-t", "room-a"}, Message: commandMessage()},
		{Tokens: []string{"echo", "second", "-t", "room-b"}, Message: commandMessage()},
	} {
		if err := instance.HandleCommand(t.Context(), invocation); err != nil {
			t.Fatalf("HandleCommand(%v) error = %v", invocation.Tokens, err)
		}
	}
	if len(recorder.echoCalls) != 2 {
		t.Fatalf("Run count = %d, want 2", len(recorder.echoCalls))
	}
	if recorder.echoCalls[0].Text != "first" || recorder.echoCalls[0].Target != "room-a" {
		t.Fatalf("first command was polluted: %+v", recorder.echoCalls[0])
	}
	if recorder.echoCalls[1].Text != "second" || recorder.echoCalls[1].Target != "room-b" {
		t.Fatalf("second command was polluted: %+v", recorder.echoCalls[1])
	}
}
