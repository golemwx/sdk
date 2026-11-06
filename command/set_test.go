package command

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golemwx/sdk"
)

type commandRecorder struct {
	mu         sync.Mutex
	echoCalls  []echoCommand
	ownerCalls int
	cronCalls  int
	listCalls  int
	contexts   int
}

type testCommands struct {
	Echo  echoCommand  `cmd:"" name:"echo" aliases:"say" help:"回复文本"`
	Owner ownerCommand `cmd:"" name:"owner" scope:"owner" help:"管理员命令"`
	Cron  cronCommand  `cmd:"" name:"cron" help:"计划管理"`
	Admin adminCommand `cmd:"" name:"admin" scope:"owner" help:"管理员命名空间"`
}

type cronCommand struct {
	Add  cronAddCommand  `cmd:"" name:"add" scope:"owner" help:"新增计划"`
	List cronListCommand `cmd:"" name:"list" help:"列出计划"`
}

type echoCommand struct {
	Text    string            `arg:"" name:"text" help:"文本"`
	Target  string            `short:"t" name:"target" required:"" help:"目标会话"`
	Enabled bool              `short:"e" name:"enabled" default:"true" help:"启用"`
	Count   int               `short:"c" name:"count" default:"1" validate:"min=1,max=9" help:"次数"`
	Mode    string            `name:"mode" enum:"fast,slow" default:"fast" help:"模式"`
	Window  time.Duration     `name:"window" default:"5s" validate:"duration" help:"窗口"`
	Timeout string            `name:"timeout" validate:"omitempty,duration" help:"超时文本"`
	Labels  map[string]string `name:"label" help:"标签"`
	Tags    []string          `short:"T" name:"tag" help:"标签列表"`
	Rest    []string          `arg:"" optional:"" name:"rest" help:"剩余参数"`
}

func (c *echoCommand) Run(ctx context.Context, recorder *commandRecorder, reply Reply) error {
	if ctx == nil {
		return errors.New("context is nil")
	}
	recorder.mu.Lock()
	recorder.echoCalls = append(recorder.echoCalls, *c)
	recorder.contexts++
	recorder.mu.Unlock()
	return reply.Text(ctx, "echo:"+c.Text)
}

type ownerCommand struct {
	Name string `arg:"" name:"name" help:"名称"`
}

func (c *ownerCommand) Run(ctx context.Context, recorder *commandRecorder, reply Reply) error {
	recorder.mu.Lock()
	recorder.ownerCalls++
	recorder.mu.Unlock()
	return reply.Text(ctx, "owner:"+c.Name)
}

type cronAddCommand struct {
	Schedule string `arg:"" name:"schedule" validate:"required,cron" help:"Cron 表达式"`
	Target   string `short:"t" name:"target" required:"" validate:"required" help:"目标会话"`
}

func (*cronAddCommand) Run(context.Context, *commandRecorder, Reply) error {
	return nil
}

type cronListCommand struct{}

func (*cronListCommand) Run(context.Context, *commandRecorder, Reply) error {
	return nil
}

type adminCommand struct {
	Child emptyCommand `cmd:"" name:"child" help:"子命令"`
}

type fakeReply struct {
	mu    sync.Mutex
	texts []string
}

func (r *fakeReply) Text(_ context.Context, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, text)
	return nil
}

func newTestSet(t *testing.T) *Set {
	t.Helper()
	set, err := NewSet(&testCommands{})
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	return set
}

// TestSchemaFromStructDeclaration 验证 Kong 模型生成完整的长短参数、位置参数、
// 可变参数、枚举、默认值、map、duration 和 owner scope。
func TestSchemaFromStructDeclaration(t *testing.T) {
	schema := newTestSet(t).Schema()
	echo := findRoot(t, schema, "echo")
	if !slices.Contains(echo.Aliases, "say") {
		t.Fatalf("echo aliases = %v, want say", echo.Aliases)
	}
	arguments := argumentsByName(echo)

	assertArgument(t, arguments["text"], Argument{
		Name: "text", Positional: true, Type: TypeString, Required: true,
	})
	assertArgument(t, arguments["target"], Argument{
		Name: "target", Short: "t", Type: TypeString, Required: true,
	})
	assertArgument(t, arguments["enabled"], Argument{
		Name: "enabled", Short: "e", Type: TypeBool, Default: "true",
	})
	assertArgument(t, arguments["count"], Argument{
		Name: "count", Short: "c", Type: TypeInt, Default: "1",
	})
	assertArgument(t, arguments["mode"], Argument{
		Name: "mode", Type: TypeEnum, Default: "fast", Enum: []string{"fast", "slow"},
	})
	assertArgument(t, arguments["window"], Argument{
		Name: "window", Type: TypeDuration, Default: "5s",
	})
	assertArgument(t, arguments["label"], Argument{Name: "label", Type: TypeMap})
	assertArgument(t, arguments["tag"], Argument{
		Name: "tag", Short: "T", Type: TypeString, Repeated: true,
	})
	assertArgument(t, arguments["rest"], Argument{
		Name: "rest", Positional: true, Type: TypeString, Repeated: true,
	})
	if echo.Scope != ScopeUser {
		t.Fatalf("echo scope = %v, want user", echo.Scope)
	}
	owner := findRoot(t, schema, "owner")
	if owner.Scope != ScopeOwner {
		t.Fatalf("owner scope = %v, want owner", owner.Scope)
	}
	cron := findRoot(t, schema, "cron")
	if cron.Children[0].Scope != ScopeOwner {
		t.Fatalf("cron add scope = %v, want owner", cron.Children[0].Scope)
	}
	admin := findRoot(t, schema, "admin")
	if admin.Children[0].Scope != ScopeOwner {
		t.Fatalf("admin child scope = %v, want inherited owner", admin.Children[0].Scope)
	}
}

// TestParseBindsLongShortFlagsPositionalsAndCollections 验证一次调用的完整绑定。
func TestParseBindsLongShortFlagsPositionalsAndCollections(t *testing.T) {
	set := newTestSet(t)
	recorder := &commandRecorder{}
	reply := &fakeReply{}
	call, err := set.Parse([]string{
		"echo", "hello",
		"-t", "room",
		"--enabled=false",
		"-c", "3",
		"--mode", "slow",
		"--window", "1m30s",
		"--label", "a=b;c=d",
		"-T", "x",
		"-T", "y",
		"one", "two",
	}, true)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := call.Run(t.Context(), recorder, reply); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.echoCalls) != 1 {
		t.Fatalf("echo run count = %d, want 1", len(recorder.echoCalls))
	}
	got := recorder.echoCalls[0]
	if got.Text != "hello" || got.Target != "room" || got.Enabled {
		t.Fatalf("bound echo = %+v", got)
	}
	if got.Count != 3 || got.Mode != "slow" || got.Window != 90*time.Second {
		t.Fatalf("bound scalar values = %+v", got)
	}
	if got.Labels["a"] != "b" || got.Labels["c"] != "d" {
		t.Fatalf("bound labels = %v", got.Labels)
	}
	if !slices.Equal(got.Tags, []string{"x", "y"}) {
		t.Fatalf("bound tags = %v", got.Tags)
	}
	if !slices.Equal(got.Rest, []string{"one", "two"}) {
		t.Fatalf("bound rest = %v", got.Rest)
	}
	if !slices.Equal(reply.texts, []string{"echo:hello"}) {
		t.Fatalf("reply texts = %v", reply.texts)
	}
	if recorder.contexts != 1 {
		t.Fatalf("context injections = %d, want 1", recorder.contexts)
	}
}

// TestParseAppliesDefaultsAndKeepsFalse 验证默认值不会覆盖显式布尔 false。
func TestParseAppliesDefaultsAndKeepsFalse(t *testing.T) {
	set := newTestSet(t)
	recorder := &commandRecorder{}
	reply := &fakeReply{}
	call, err := set.Parse([]string{"echo", "hello", "--target", "room", "--enabled=false"}, true)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := call.Run(t.Context(), recorder, reply); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := recorder.echoCalls[0]
	if got.Enabled {
		t.Fatal("explicit false was replaced by the true default")
	}
	if got.Count != 1 || got.Mode != "fast" || got.Window != 5*time.Second {
		t.Fatalf("defaults = %+v", got)
	}
	if len(got.Labels) != 0 || len(got.Tags) != 0 || len(got.Rest) != 0 {
		t.Fatalf("unset collections = labels %v tags %v rest %v", got.Labels, got.Tags, got.Rest)
	}
}

// TestParseRejectsInvalidValues 验证枚举、Kong 类型转换和 validator 规则。
func TestParseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		tokens     []string
		wantField  string
		wantReason string
	}{
		{
			name:       "invalid enum",
			tokens:     []string{"echo", "hello", "-t", "room", "--mode", "warp"},
			wantReason: "参数值无效",
		},
		{
			name:       "invalid duration type",
			tokens:     []string{"echo", "hello", "-t", "room", "--window", "soon"},
			wantReason: "命令参数无效",
		},
		{
			name:       "validator min",
			tokens:     []string{"echo", "hello", "-t", "room", "-c", "0"},
			wantField:  "count",
			wantReason: "不能小于 1",
		},
		{
			name:       "validator duration tag",
			tokens:     []string{"echo", "hello", "-t", "room", "--timeout", "later"},
			wantField:  "timeout",
			wantReason: "不是有效的时长",
		},
		{
			name:       "validator cron",
			tokens:     []string{"cron", "add", "bad cron", "-t", "room"},
			wantField:  "schedule",
			wantReason: "不是有效的 Cron 表达式",
		},
		{
			name:       "missing required positional",
			tokens:     []string{"echo", "-t", "room"},
			wantReason: "缺少必填参数",
		},
		{
			name:       "missing required flag",
			tokens:     []string{"echo", "hello"},
			wantReason: "缺少必填参数",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &commandRecorder{}
			_, err := newTestSet(t).Parse(test.tokens, true)
			if err == nil {
				t.Fatal("Parse() unexpectedly succeeded")
			}
			var business *sdk.Error
			if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
				t.Fatalf("Parse() error = %v, want InvalidArgument", err)
			}
			if !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("Parse() error = %q, want reason %q", err, test.wantReason)
			}
			if test.wantField != "" && !strings.Contains(err.Error(), test.wantField) {
				t.Fatalf("Parse() error = %q, want field %q", err, test.wantField)
			}
			if len(recorder.echoCalls) != 0 {
				t.Fatal("invalid command reached Run")
			}
		})
	}
}

// TestParseHelpDoesNotRunBusiness 验证帮助来自 Kong 模型且不调用 Run。
func TestParseHelpDoesNotRunBusiness(t *testing.T) {
	set := newTestSet(t)
	call, err := set.Parse([]string{"echo", "--help"}, true)
	if err != nil {
		t.Fatalf("Parse(--help) error = %v", err)
	}
	help := call.Help()
	if help == "" {
		t.Fatal("help text is empty")
	}
	for _, want := range []string{"golem echo", "-t", "--target", "目标会话"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help text %q does not contain %q", help, want)
		}
	}
	root, err := set.Parse([]string{"--help"}, true)
	if err != nil {
		t.Fatalf("root Parse(--help) error = %v", err)
	}
	for _, want := range []string{"echo", "owner", "cron"} {
		if !strings.Contains(root.Help(), want) {
			t.Fatalf("root help %q does not contain %q", root.Help(), want)
		}
	}
}

// TestParseTerminatorKeepsHelpTextAsArgument 验证 `--` 之后的 --help/-h 是业务参数。
func TestParseTerminatorKeepsHelpTextAsArgument(t *testing.T) {
	set := newTestSet(t)
	recorder := &commandRecorder{}
	call, err := set.Parse([]string{"echo", "-t", "room", "--", "--help"}, true)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if call.Help() != "" {
		t.Fatalf("terminator unexpectedly produced help: %q", call.Help())
	}
	if err := call.Run(t.Context(), recorder, &fakeReply{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(recorder.echoCalls) != 1 || recorder.echoCalls[0].Text != "--help" {
		t.Fatalf("terminator bound command = %+v", recorder.echoCalls)
	}
}

// TestOwnerCommandsAreRejectedBeforeParsing 验证 owner 命令在参数解析前拒绝。
func TestOwnerCommandsAreRejectedBeforeParsing(t *testing.T) {
	set := newTestSet(t)
	for _, tokens := range [][]string{
		{"owner"},
		{"owner", "name"},
		{"cron", "add"},
		{"cron", "add", "bad cron", "-t", "room"},
		{"admin", "child"},
	} {
		_, err := set.Parse(tokens, false)
		var business *sdk.Error
		if !errors.As(err, &business) || business.Category != sdk.CategoryPermissionDenied {
			t.Fatalf("Parse(%v) error = %v, want PermissionDenied", tokens, err)
		}
	}
	recorder := &commandRecorder{}
	reply := &fakeReply{}
	call, err := set.Parse([]string{"cron", "list"}, false)
	if err != nil {
		t.Fatalf("non-owner list Parse() error = %v", err)
	}
	if err := call.Run(t.Context(), recorder, reply); err != nil {
		t.Fatalf("non-owner list Run() error = %v", err)
	}
	echoCall, err := set.Parse([]string{"echo", "owner", "-t", "room"}, false)
	if err != nil {
		t.Fatalf("non-owner echo with owner-like text error = %v", err)
	}
	if err := echoCall.Run(t.Context(), recorder, reply); err != nil {
		t.Fatalf("non-owner echo with owner-like text Run() error = %v", err)
	}
}

// TestInvocationsUseIndependentCommandValues 验证连续调用不会复用绑定值。
func TestInvocationsUseIndependentCommandValues(t *testing.T) {
	set := newTestSet(t)
	recorder := &commandRecorder{}
	first, err := set.Parse([]string{"echo", "first", "-t", "room-a", "-c", "2"}, true)
	if err != nil {
		t.Fatalf("first Parse() error = %v", err)
	}
	second, err := set.Parse([]string{"echo", "second", "-t", "room-b", "-c", "4"}, true)
	if err != nil {
		t.Fatalf("second Parse() error = %v", err)
	}
	if err := first.Run(t.Context(), recorder, &fakeReply{}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := second.Run(t.Context(), recorder, &fakeReply{}); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if len(recorder.echoCalls) != 2 {
		t.Fatalf("run count = %d, want 2", len(recorder.echoCalls))
	}
	if recorder.echoCalls[0].Text != "first" || recorder.echoCalls[0].Target != "room-a" || recorder.echoCalls[0].Count != 2 {
		t.Fatalf("first call was polluted: %+v", recorder.echoCalls[0])
	}
	if recorder.echoCalls[1].Text != "second" || recorder.echoCalls[1].Target != "room-b" || recorder.echoCalls[1].Count != 4 {
		t.Fatalf("second call was polluted: %+v", recorder.echoCalls[1])
	}
}

// TestRootAliasesRouteLikeCanonicalName 验证根别名与正式名称使用同一命令值。
func TestRootAliasesRouteLikeCanonicalName(t *testing.T) {
	set := newTestSet(t)
	recorder := &commandRecorder{}
	call, err := set.Parse([]string{"say", "hello", "-t", "room"}, true)
	if err != nil {
		t.Fatalf("alias Parse() error = %v", err)
	}
	if err := call.Run(t.Context(), recorder, &fakeReply{}); err != nil {
		t.Fatalf("alias Run() error = %v", err)
	}
	if len(recorder.echoCalls) != 1 || recorder.echoCalls[0].Text != "hello" {
		t.Fatalf("alias did not route to echo: %+v", recorder.echoCalls)
	}
}

type emptyCommand struct{}

type duplicateRootCommands struct {
	First  emptyCommand `cmd:"" name:"same"`
	Second emptyCommand `cmd:"" name:"same"`
}

type duplicateAliasCommands struct {
	First  emptyCommand `cmd:"" name:"one" aliases:"shared"`
	Second emptyCommand `cmd:"" name:"two" aliases:"shared"`
}

type aliasCollidesWithNameCommands struct {
	First  emptyCommand `cmd:"" name:"one" aliases:"two"`
	Second emptyCommand `cmd:"" name:"two"`
}

type reservedHelpNameCommands struct {
	Do reservedHelpNameCommand `cmd:"" name:"do"`
}

type reservedHelpNameCommand struct {
	Help string `name:"help"`
}

func (*reservedHelpNameCommand) Run() error { return nil }

type reservedHelpShortCommands struct {
	Do reservedHelpShortCommand `cmd:"" name:"do"`
}

type reservedHelpShortCommand struct {
	Host string `short:"h" name:"host"`
}

func (*reservedHelpShortCommand) Run() error { return nil }

// TestDuplicateRootsAndAliasesAreRejected 验证重复根、重复别名和别名撞名。
func TestDuplicateRootsAndAliasesAreRejected(t *testing.T) {
	tests := []struct {
		name     string
		commands any
	}{
		{name: "duplicate roots", commands: &duplicateRootCommands{}},
		{name: "duplicate aliases", commands: &duplicateAliasCommands{}},
		{name: "alias collides with name", commands: &aliasCollidesWithNameCommands{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSet(test.commands); err == nil {
				t.Fatal("NewSet() unexpectedly accepted duplicate command names")
			}
		})
	}
}

// TestReservedHelpFlagsAreRejected 验证插件不能占用自动帮助的名称和短参数。
func TestReservedHelpFlagsAreRejected(t *testing.T) {
	for _, commands := range []any{&reservedHelpNameCommands{}, &reservedHelpShortCommands{}} {
		if _, err := NewSet(commands); err == nil {
			t.Fatalf("NewSet(%T) unexpectedly accepted a reserved help flag", commands)
		}
	}
}

func findRoot(t *testing.T, schema Schema, name string) Node {
	t.Helper()
	for _, root := range schema.Roots {
		if root.Name == name {
			return root
		}
	}
	t.Fatalf("root %q not found in %+v", name, schema.Roots)
	return Node{}
}

func argumentsByName(node Node) map[string]Argument {
	result := make(map[string]Argument, len(node.Arguments))
	for _, argument := range node.Arguments {
		result[argument.Name] = argument
	}
	return result
}

func assertArgument(t *testing.T, got Argument, want Argument) {
	t.Helper()
	got.Help = ""
	want.Help = ""
	if len(got.Enum) == 0 {
		got.Enum = nil
	}
	if len(want.Enum) == 0 {
		want.Enum = nil
	}
	if got.Name != want.Name ||
		got.Short != want.Short ||
		got.Positional != want.Positional ||
		got.Type != want.Type ||
		got.Required != want.Required ||
		got.Repeated != want.Repeated ||
		got.Default != want.Default ||
		!slices.Equal(got.Enum, want.Enum) {
		t.Fatalf("argument = %#v, want %#v", got, want)
	}
}
