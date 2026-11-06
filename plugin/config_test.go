package plugin

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/contact"
	"github.com/golemwx/sdk/message"
)

type testConfig struct {
	Name      string            `toml:"name"`
	Threshold int               `toml:"threshold" validate:"min=2,max=10"`
	Enabled   bool              `toml:"enabled"`
	Count     int               `toml:"count"`
	Items     []string          `toml:"items"`
	Labels    map[string]string `toml:"labels"`
	Optional  *testOptional     `toml:"optional"`
	Jobs      []testJob         `toml:"jobs"`
}

type testOptional struct {
	Value string `toml:"value"`
}

type testJob struct {
	Name     string `toml:"name" validate:"required"`
	Schedule string `toml:"schedule" validate:"required,cron"`
}

func validConfig() testConfig {
	return testConfig{
		Name:      "default",
		Threshold: 2,
		Enabled:   false,
		Count:     0,
		Items:     []string{},
		Labels:    map[string]string{},
	}
}

type configTestPlugin struct {
	*Base
	*Config[testConfig]

	mu          sync.Mutex
	changes     []configChange
	mutateCalls int
	callbackErr error
	callback    func(context.Context, testConfig, testConfig) error
}

type configChange struct {
	previous testConfig
	next     testConfig
}

func (*configTestPlugin) Metadata() Metadata {
	return Metadata{ID: "config-fixture"}
}

func (p *configTestPlugin) OnConfigChanged(ctx context.Context, previous, next testConfig) error {
	p.mu.Lock()
	p.changes = append(p.changes, configChange{previous: previous, next: next})
	callback := p.callback
	callbackErr := p.callbackErr
	p.mu.Unlock()
	if callback != nil {
		return callback(ctx, previous, next)
	}
	return callbackErr
}

func (p *configTestPlugin) changeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.changes)
}

type validatingConfigPlugin struct {
	*Base
	*Config[testConfig]
}

func (*validatingConfigPlugin) Metadata() Metadata {
	return Metadata{ID: "validating-config-fixture"}
}

func (*validatingConfigPlugin) ValidateConfig(_ context.Context, candidate testConfig) error {
	if candidate.Name == "invalid" {
		return sdk.NewError(sdk.CategoryInvalidArgument, "plugin.config.validate", "计划名称不能是 invalid")
	}
	return nil
}

type fakeConfigClient struct {
	mu sync.Mutex

	file        []byte
	readErr     error
	saveErr     error
	writeOnSave bool
	readCalls   int
	saveCalls   int
	saveStarted chan struct{}
	saveRelease chan struct{}
}

func newFakeConfigClient(file []byte) *fakeConfigClient {
	return &fakeConfigClient{file: append([]byte(nil), file...), writeOnSave: true}
}

func (c *fakeConfigClient) Read(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readCalls++
	if c.readErr != nil {
		return nil, c.readErr
	}
	return append([]byte(nil), c.file...), nil
}

func (c *fakeConfigClient) Save(_ context.Context, data []byte) error {
	c.mu.Lock()
	c.saveCalls++
	started := c.saveStarted
	release := c.saveRelease
	c.saveStarted = nil
	err := c.saveErr
	write := c.writeOnSave
	if write {
		c.file = append([]byte(nil), data...)
	}
	c.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	return err
}

func (c *fakeConfigClient) fileData() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.file...)
}

func (c *fakeConfigClient) counts() (reads, saves int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readCalls, c.saveCalls
}

type configTestMessages struct{}

func (*configTestMessages) SendText(context.Context, string, string, []string) (*message.SendResult, error) {
	return &message.SendResult{}, nil
}

func (*configTestMessages) SendApp(context.Context, string, message.App) (*message.SendResult, error) {
	return nil, nil
}

func (*configTestMessages) SendMedia(context.Context, string, message.Upload) (*message.SendResult, error) {
	return nil, nil
}

func (*configTestMessages) Forward(context.Context, string, message.Message) (*message.SendResult, error) {
	return nil, nil
}

func (*configTestMessages) Revoke(context.Context, message.SendResult) error {
	return nil
}

func (*configTestMessages) Download(context.Context, message.Message) (io.ReadCloser, error) {
	return nil, nil
}

func ownerContext() context.Context {
	return command.WithAuthorization(context.Background(), command.Authorization{Owner: true})
}

func mustEncode[T any](t *testing.T, value T) []byte {
	t.Helper()
	data, err := encodeConfig(value)
	if err != nil {
		t.Fatalf("encodeConfig() error = %v", err)
	}
	return data
}

func newConfigInstance(t *testing.T, implementation Plugin, client ConfigClient, initial []byte) *runtime {
	t.Helper()
	instance, err := newRuntime(implementation)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	host := NewHost(nil, &configTestMessages{}, nil, nil, nil, nil, nil)
	host.configClient = client
	if err := instance.Initialize(context.Background(), InitializeOptions{
		Host:    host,
		Config:  initial,
		DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	return instance
}

func newConfigPlugin(defaults testConfig) *configTestPlugin {
	return &configTestPlugin{
		Base:   NewBase(),
		Config: NewConfig(defaults),
	}
}

func TestConfigDescribeReportsDefaultsAndZeroValues(t *testing.T) {
	defaults := validConfig()
	defaults.Items = nil
	defaults.Labels = nil
	defaults.Optional = nil
	plugin := newConfigPlugin(defaults)
	instance, err := newRuntime(plugin)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	descriptor, err := instance.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if !descriptor.HasConfig {
		t.Fatal("Describe() did not report HasConfig")
	}
	text := string(descriptor.DefaultConfig)
	for _, want := range []string{
		"name = 'default'",
		"threshold = 2",
		"enabled = false",
		"count = 0",
		"items = []",
		"[labels]",
		"# 字段 optional 未设置（nil）",
		"# optional.value = ''",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("DefaultConfig missing %q:\n%s", want, text)
		}
	}
	decoded, err := decodeConfig[testConfig](descriptor.DefaultConfig)
	if err != nil {
		t.Fatalf("decodeConfig(DefaultConfig) error = %v", err)
	}
	if decoded.Optional != nil {
		t.Fatalf("nil optional pointer was not preserved: %+v", decoded.Optional)
	}
	if decoded.Enabled || decoded.Count != 0 || len(decoded.Items) != 0 || len(decoded.Labels) != 0 {
		t.Fatalf("zero values did not round-trip: %+v", decoded)
	}
}

func TestConfigExistingFileWinsOverDefaults(t *testing.T) {
	defaults := validConfig()
	custom := defaults
	custom.Name = "custom"
	custom.Threshold = 7
	file := mustEncode(t, custom)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	instance := newConfigInstance(t, plugin, client, file)
	if got := plugin.Snapshot(); got.Name != "custom" || got.Threshold != 7 {
		t.Fatalf("Snapshot() = %+v, want existing file values", got)
	}
	descriptor, err := instance.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if !strings.Contains(string(descriptor.DefaultConfig), "name = 'default'") {
		t.Fatal("Describe() default config was replaced by the existing file")
	}
}

func TestConfigInvalidInitialFileCanRetryInitialize(t *testing.T) {
	defaults := validConfig()
	client := newFakeConfigClient(mustEncode(t, defaults))
	plugin := newConfigPlugin(defaults)
	instance, err := newRuntime(plugin)
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	host := NewHost(nil, &configTestMessages{}, nil, nil, nil, nil, nil)
	host.configClient = client

	invalid := validConfig()
	invalid.Threshold = 1
	err = instance.Initialize(context.Background(), InitializeOptions{
		Host:    host,
		Config:  mustEncode(t, invalid),
		DataDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Initialize() accepted an invalid initial config")
	}
	if err := instance.Initialize(context.Background(), InitializeOptions{
		Host:    host,
		Config:  mustEncode(t, defaults),
		DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() retry error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("Snapshot().Name = %q, want default", got)
	}
}

func TestConfigStrictDecodeRejectsUnknownAndMalformedFields(t *testing.T) {
	client := newFakeConfigClient(mustEncode(t, validConfig()))
	plugin := newConfigPlugin(validConfig())
	instance := newConfigInstance(t, plugin, client, mustEncode(t, validConfig()))

	err := instance.ValidateConfig(context.Background(), []byte("unknown = 1\n"))
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
		t.Fatalf("ValidateConfig(unknown) error = %v, want InvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("ValidateConfig(unknown) error = %q", err)
	}

	err = instance.ValidateConfig(context.Background(), []byte("threshold = 'bad'\n"))
	if !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
		t.Fatalf("ValidateConfig(malformed) error = %v, want InvalidArgument", err)
	}
	if strings.Contains(err.Error(), "testConfig") {
		t.Fatalf("decode error leaked Go type path: %q", err)
	}
}

func TestConfigValidationUsesTagsAndBusinessRules(t *testing.T) {
	client := newFakeConfigClient(mustEncode(t, validConfig()))
	plugin := &validatingConfigPlugin{Base: NewBase(), Config: NewConfig(validConfig())}
	instance := newConfigInstance(t, plugin, client, mustEncode(t, validConfig()))

	tooSmall := validConfig()
	tooSmall.Threshold = 1
	err := instance.ValidateConfig(context.Background(), mustEncode(t, tooSmall))
	if err == nil || !strings.Contains(err.Error(), "threshold") || !strings.Contains(err.Error(), "不能小于 2") {
		t.Fatalf("ValidateConfig(too small) error = %v", err)
	}

	business := validConfig()
	business.Name = "invalid"
	err = instance.ValidateConfig(context.Background(), mustEncode(t, business))
	if err == nil || !strings.Contains(err.Error(), "计划名称不能是 invalid") {
		t.Fatalf("ValidateConfig(business) error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("ValidateConfig() changed current config: %q", got)
	}
}

func TestConfigSnapshotAndDefaultsAreDeepCopied(t *testing.T) {
	defaults := validConfig()
	defaults.Items = []string{"default"}
	defaults.Labels = map[string]string{"mode": "default"}
	defaults.Optional = &testOptional{Value: "default"}
	plugin := newConfigPlugin(defaults)
	defaults.Items[0] = "changed"
	defaults.Labels["mode"] = "changed"
	defaults.Optional.Value = "changed"

	first := plugin.Snapshot()
	if first.Items[0] != "default" || first.Labels["mode"] != "default" || first.Optional.Value != "default" {
		t.Fatalf("Snapshot() shares defaults: %+v", first)
	}
	first.Items[0] = "mutated"
	first.Labels["mode"] = "mutated"
	first.Optional.Value = "mutated"

	second := plugin.Snapshot()
	if second.Items[0] != "default" || second.Labels["mode"] != "default" || second.Optional.Value != "default" {
		t.Fatalf("Snapshot() shares current state: %+v", second)
	}
}

func TestConfigUpdateCandidateIsIsolated(t *testing.T) {
	defaults := validConfig()
	defaults.Items = []string{"default"}
	defaults.Labels = map[string]string{"mode": "default"}
	defaults.Optional = &testOptional{Value: "default"}
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	var retained *testConfig
	if err := plugin.Update(ownerContext(), func(next *testConfig) error {
		retained = next
		next.Name = "updated"
		return nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	retained.Items[0] = "mutated"
	retained.Labels["mode"] = "mutated"
	retained.Optional.Value = "mutated"

	got := plugin.Snapshot()
	if got.Name != "updated" || got.Items[0] != "default" || got.Labels["mode"] != "default" || got.Optional.Value != "default" {
		t.Fatalf("Update() published shared candidate references: %+v", got)
	}
}

func TestConfigUpdateSavesPublishesAndCallsHandler(t *testing.T) {
	defaults := validConfig()
	defaults.Items = nil
	defaults.Labels = nil
	defaults.Optional = nil
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	instance := newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		plugin.mu.Lock()
		plugin.mutateCalls++
		plugin.mu.Unlock()
		next.Jobs = append(next.Jobs, testJob{Name: "daily", Schedule: "0 8 * * *"})
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := plugin.Snapshot(); len(got.Jobs) != 1 || got.Jobs[0].Name != "daily" {
		t.Fatalf("Snapshot() = %+v, want one job", got)
	}
	if plugin.changeCount() != 1 {
		t.Fatalf("OnConfigChanged calls = %d, want 1", plugin.changeCount())
	}
	if reads, saves := client.counts(); reads == 0 || saves != 1 {
		t.Fatalf("client calls reads=%d saves=%d, want at least one read and one save", reads, saves)
	}
	_ = instance
}

func TestConfigUpdateFailureKeepsOldValue(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.saveErr = sdk.NewError(sdk.CategoryInvalidArgument, "config.save", "write rejected")
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		next.Threshold = 1
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "threshold") {
		t.Fatalf("Update(invalid) error = %v", err)
	}
	if got := plugin.Snapshot().Threshold; got != 2 {
		t.Fatalf("Update(invalid) changed config: %d", got)
	}
	if plugin.changeCount() != 0 {
		t.Fatalf("OnConfigChanged calls = %d, want 0", plugin.changeCount())
	}

	err = plugin.Update(ownerContext(), func(next *testConfig) error {
		next.Name = "new"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "write rejected") {
		t.Fatalf("Update(save failure) error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("save failure changed config: %q", got)
	}
	if plugin.changeCount() != 0 {
		t.Fatalf("save failure called OnConfigChanged")
	}
}

func TestConfigUnknownSaveAlignsWithoutRepeatingMutation(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.saveErr = unknownSaveError()
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		plugin.mu.Lock()
		plugin.mutateCalls++
		plugin.mu.Unlock()
		next.Jobs = append(next.Jobs, testJob{Name: "daily", Schedule: "0 8 * * *"})
		return nil
	})
	if err != nil {
		t.Fatalf("Update() with lost ACK error = %v", err)
	}
	if plugin.mutateCalls != 1 {
		t.Fatalf("mutation callback calls = %d, want 1", plugin.mutateCalls)
	}
	if plugin.changeCount() != 1 {
		t.Fatalf("OnConfigChanged calls = %d, want 1", plugin.changeCount())
	}
	if got := plugin.Snapshot(); len(got.Jobs) != 1 {
		t.Fatalf("Snapshot() = %+v, want one job", got)
	}
}

func TestConfigUnknownSaveWithoutWriteKeepsOldValue(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.writeOnSave = false
	client.saveErr = unknownSaveError()
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		plugin.mu.Lock()
		plugin.mutateCalls++
		plugin.mu.Unlock()
		next.Jobs = append(next.Jobs, testJob{Name: "daily", Schedule: "0 8 * * *"})
		return nil
	})
	if err == nil {
		t.Fatal("Update() with unconfirmed save unexpectedly succeeded")
	}
	if plugin.mutateCalls != 1 {
		t.Fatalf("mutation callback calls = %d, want 1", plugin.mutateCalls)
	}
	if plugin.changeCount() != 0 {
		t.Fatalf("unconfirmed save called OnConfigChanged")
	}
	if got := plugin.Snapshot(); len(got.Jobs) != 0 {
		t.Fatalf("unconfirmed save changed config: %+v", got)
	}
}

func TestConfigUnimplementedSaveDoesNotPauseBusiness(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.readErr = sdk.NewError(sdk.CategoryUnimplemented, "config.read", "not implemented")
	client.saveErr = sdk.NewError(sdk.CategoryUnimplemented, "config.save", "not implemented")
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		next.Name = "new"
		return nil
	})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryUnimplemented {
		t.Fatalf("Update() error = %v, want Unimplemented", err)
	}
	if got := plugin.Status(); got != StatusReady {
		t.Fatalf("Status() = %v, want StatusReady", got)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("unimplemented save changed config: %q", got)
	}
}

func TestConfigExternalFileChangeRequiresReload(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	external := defaults
	external.Name = "external"
	client.mu.Lock()
	client.file = mustEncode(t, external)
	client.mu.Unlock()

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		next.Name = "local"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "reload") {
		t.Fatalf("Update() error = %v, want reload precondition", err)
	}
	if _, saves := client.counts(); saves != 0 {
		t.Fatalf("external change still called Save %d times", saves)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("external change modified current config: %q", got)
	}
}

func TestConfigSetConfigPublishesWithoutSaving(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	next := defaults
	next.Name = "reloaded"
	next.Enabled = false
	next.Count = 0
	if err := plugin.SetConfig(context.Background(), mustEncode(t, next)); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if got := plugin.Snapshot(); got.Name != "reloaded" || got.Enabled || got.Count != 0 {
		t.Fatalf("Snapshot() = %+v, want reloaded zero-value config", got)
	}
	if plugin.changeCount() != 1 {
		t.Fatalf("OnConfigChanged calls = %d, want 1", plugin.changeCount())
	}
	if _, saves := client.counts(); saves != 0 {
		t.Fatalf("SetConfig() called Save %d times", saves)
	}
}

func TestConfigSaveReplacesAndValidates(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	next := defaults
	next.Name = "saved"
	next.Jobs = []testJob{{Name: "daily", Schedule: "0 8 * * *"}}
	if err := plugin.Save(ownerContext(), next); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if got := plugin.Snapshot(); got.Name != "saved" || len(got.Jobs) != 1 {
		t.Fatalf("Snapshot() = %+v, want saved config", got)
	}

	invalid := next
	invalid.Threshold = 1
	if err := plugin.Save(ownerContext(), invalid); err == nil || !strings.Contains(err.Error(), "threshold") {
		t.Fatalf("Save(invalid) error = %v", err)
	}
	if got := plugin.Snapshot().Threshold; got != 2 {
		t.Fatalf("Save(invalid) changed config: %d", got)
	}
}

func TestConfigSaveRoundTripsZeroValues(t *testing.T) {
	defaults := validConfig()
	defaults.Enabled = true
	defaults.Count = 9
	defaults.Items = []string{"default"}
	defaults.Labels = map[string]string{"mode": "default"}
	defaults.Optional = &testOptional{Value: "default"}
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	next := validConfig()
	if err := plugin.Save(ownerContext(), next); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	decoded, err := decodeConfig[testConfig](client.fileData())
	if err != nil {
		t.Fatalf("decodeConfig(saved file) error = %v", err)
	}
	if decoded.Enabled || decoded.Count != 0 || len(decoded.Items) != 0 || len(decoded.Labels) != 0 {
		t.Fatalf("saved zero values did not round-trip: %+v", decoded)
	}
	if decoded.Optional != nil {
		t.Fatalf("saved nil optional pointer did not round-trip: %+v", decoded.Optional)
	}
	text := string(client.fileData())
	for _, want := range []string{"enabled = false", "count = 0", "items = []", "[labels]", "# 字段 optional 未设置（nil）"} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %q:\n%s", want, text)
		}
	}
}

func TestConfigCallbackFailureKeepsSavedValueAndMarksApplyFailed(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	plugin.callbackErr = sdk.NewError(sdk.CategoryInternal, "plugin.apply", "resource rebuild failed")
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(ownerContext(), func(next *testConfig) error {
		next.Name = "saved"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "配置已保存，但应用失败") {
		t.Fatalf("Update() error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "saved" {
		t.Fatalf("callback failure lost saved config: %q", got)
	}
	if got := plugin.Status(); got != StatusApplyFailed {
		t.Fatalf("Status() = %v, want StatusApplyFailed", got)
	}
}

func TestConfigCallbackCannotRecursivelyUpdate(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	var once sync.Once
	var recursiveErr error
	plugin.callback = func(ctx context.Context, _, _ testConfig) error {
		once.Do(func() {
			recursiveErr = plugin.Update(ctx, func(next *testConfig) error {
				next.Name = "recursive"
				return nil
			})
		})
		return nil
	}
	newConfigInstance(t, plugin, client, file)

	done := make(chan error, 1)
	go func() {
		done <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Name = "outer"
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("outer Update() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recursive Update() deadlocked")
	}
	if recursiveErr == nil || !strings.Contains(recursiveErr.Error(), "递归") {
		t.Fatalf("recursive Update() error = %v, want recursion rejection", recursiveErr)
	}
	if got := plugin.Snapshot().Name; got != "outer" {
		t.Fatalf("Snapshot().Name = %q, want outer", got)
	}
}

func TestConfigSaveDoesNotBlockSnapshotOrValidation(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.saveStarted = make(chan struct{})
	client.saveRelease = make(chan struct{})
	plugin := newConfigPlugin(defaults)
	instance := newConfigInstance(t, plugin, client, file)

	done := make(chan error, 1)
	go func() {
		done <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Name = "saving"
			return nil
		})
	}()
	select {
	case <-client.saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Save() did not start")
	}

	snapshotDone := make(chan testConfig, 1)
	go func() { snapshotDone <- plugin.Snapshot() }()
	select {
	case got := <-snapshotDone:
		if got.Name != "default" {
			t.Fatalf("Snapshot() during save = %q, want old value", got.Name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Snapshot() blocked behind Save()")
	}

	validateDone := make(chan error, 1)
	validBytes := mustEncode(t, defaults)
	go func() { validateDone <- instance.ValidateConfig(context.Background(), validBytes) }()
	select {
	case err := <-validateDone:
		if err != nil {
			t.Fatalf("ValidateConfig() during save error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ValidateConfig() blocked behind Save()")
	}

	close(client.saveRelease)
	if err := <-done; err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestConfigConcurrentUpdatesAreSerialized(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	client.saveStarted = make(chan struct{})
	client.saveRelease = make(chan struct{})
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	first := make(chan error, 1)
	go func() {
		first <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Items = append(next.Items, "first")
			return nil
		})
	}()
	select {
	case <-client.saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first Update() did not reach Save")
	}

	second := make(chan error, 1)
	go func() {
		second <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Items = append(next.Items, "second")
			return nil
		})
	}()
	select {
	case err := <-second:
		t.Fatalf("second Update() returned before first save completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(client.saveRelease)
	if err := <-first; err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Update() error = %v", err)
	}
	if got := plugin.Snapshot(); len(got.Items) != 2 {
		t.Fatalf("Snapshot().Items = %v, want both updates", got.Items)
	}
}

func TestConfigConcurrentUpdateWaitsDuringMutation(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	mutateStarted := make(chan struct{})
	mutateRelease := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- plugin.Update(ownerContext(), func(next *testConfig) error {
			close(mutateStarted)
			<-mutateRelease
			next.Items = append(next.Items, "first")
			return nil
		})
	}()
	select {
	case <-mutateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first Update() did not enter mutation")
	}

	second := make(chan error, 1)
	go func() {
		second <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Items = append(next.Items, "second")
			return nil
		})
	}()
	select {
	case err := <-second:
		t.Fatalf("second Update() returned before first mutation completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(mutateRelease)
	if err := <-first; err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Update() error = %v", err)
	}
	if got := plugin.Snapshot().Items; len(got) != 2 {
		t.Fatalf("Snapshot().Items = %v, want both updates", got)
	}
}

func TestConfigConcurrentUpdateWaitsDuringCallback(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	var once sync.Once
	plugin.callback = func(context.Context, testConfig, testConfig) error {
		once.Do(func() {
			close(callbackStarted)
			<-callbackRelease
		})
		return nil
	}
	newConfigInstance(t, plugin, client, file)

	first := make(chan error, 1)
	go func() {
		first <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Name = "first"
			return nil
		})
	}()
	select {
	case <-callbackStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first Update() did not enter OnConfigChanged")
	}

	second := make(chan error, 1)
	go func() {
		second <- plugin.Update(ownerContext(), func(next *testConfig) error {
			next.Name = "second"
			return nil
		})
	}()
	select {
	case err := <-second:
		t.Fatalf("second Update() returned before first callback completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(callbackRelease)
	if err := <-first; err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second Update() error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "second" {
		t.Fatalf("Snapshot().Name = %q, want second", got)
	}
}

func TestConfigUpdateRequiresOwner(t *testing.T) {
	defaults := validConfig()
	file := mustEncode(t, defaults)
	client := newFakeConfigClient(file)
	plugin := newConfigPlugin(defaults)
	newConfigInstance(t, plugin, client, file)

	err := plugin.Update(context.Background(), func(next *testConfig) error {
		next.Name = "denied"
		return nil
	})
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryPermissionDenied {
		t.Fatalf("Update() without owner error = %v, want PermissionDenied", err)
	}
	if got := plugin.Snapshot().Name; got != "default" {
		t.Fatalf("unauthorized Update changed config: %q", got)
	}
}

func unknownSaveError() error {
	value := sdk.NewError(sdk.CategoryUnavailable, "config.save", "response lost")
	value.Outcome = sdk.OutcomeUnknown
	return value
}

func TestConfigPendingAllowsOwnerRepairCommand(t *testing.T) {
	plugin := newPendingPlugin()
	initial := mustEncode(t, pendingConfig{})
	client := newFakeConfigClient(initial)
	instance := newConfigInstance(t, plugin, client, initial)
	if got := plugin.Status(); got != StatusPending {
		t.Fatalf("Status() = %v, want StatusPending", got)
	}
	if err := instance.Load(context.Background()); err == nil {
		t.Fatal("Load() succeeded while config is pending")
	}
	if err := instance.Start(context.Background()); err == nil {
		t.Fatal("Start() succeeded while config is pending")
	}
	if err := instance.HandleCommand(context.Background(), command.Invocation{
		Tokens:  []string{"fix"},
		Owner:   false,
		Message: ownerMessage(),
	}); err == nil {
		t.Fatal("non-owner repair command was accepted")
	}
	if err := instance.HandleCommand(context.Background(), command.Invocation{
		Tokens:  []string{"fix"},
		Owner:   true,
		Message: ownerMessage(),
	}); err != nil {
		t.Fatalf("owner repair command error = %v", err)
	}
	if got := plugin.Snapshot().Name; got != "fixed" {
		t.Fatalf("Snapshot().Name = %q, want fixed", got)
	}
	if got := plugin.Status(); got != StatusReady {
		t.Fatalf("Status() = %v, want StatusReady", got)
	}
	if err := instance.Load(context.Background()); err != nil {
		t.Fatalf("Load() after repair error = %v", err)
	}
	if err := instance.Start(context.Background()); err != nil {
		t.Fatalf("Start() after repair error = %v", err)
	}
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := instance.Unload(context.Background()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestConfigPendingFromEmptyInitialConfig(t *testing.T) {
	plugin := newPendingPlugin()
	client := newFakeConfigClient(mustEncode(t, pendingConfig{}))
	newConfigInstance(t, plugin, client, nil)
	if got := plugin.Status(); got != StatusPending {
		t.Fatalf("Status() = %v, want StatusPending", got)
	}
	if got := plugin.Snapshot().Name; got != "" {
		t.Fatalf("Snapshot().Name = %q, want empty pending default", got)
	}
}

func ownerMessage() *message.Message {
	return &message.Message{
		Type:     message.TypeText,
		Sender:   contact.Contact{Username: "owner", Kind: contact.KindPersonal},
		Receiver: contact.Contact{Username: "bot", Kind: contact.KindSelf},
		Content:  "/fix",
	}
}

type pendingConfig struct {
	Name string `toml:"name" validate:"required"`
}

type pendingPlugin struct {
	*Base
	*Config[pendingConfig]

	mu      sync.Mutex
	changes int
}

func newPendingPlugin() *pendingPlugin {
	return &pendingPlugin{Base: NewBase(), Config: NewConfig(pendingConfig{})}
}

func (*pendingPlugin) Metadata() Metadata {
	return Metadata{ID: "pending-config-fixture"}
}

func (p *pendingPlugin) Commands() any {
	return &pendingCommands{}
}

func (p *pendingPlugin) OnConfigChanged(context.Context, pendingConfig, pendingConfig) error {
	p.mu.Lock()
	p.changes++
	p.mu.Unlock()
	return nil
}

type pendingCommands struct {
	Fix pendingFixCommand `cmd:"" name:"fix" scope:"owner" help:"修复配置"`
}

type pendingFixCommand struct{}

func (pendingFixCommand) Run(ctx context.Context, plugin *pendingPlugin, _ command.Reply) error {
	return plugin.Config.Update(ctx, func(next *pendingConfig) error {
		next.Name = "fixed"
		return nil
	})
}

func TestConfigNoConfigPluginKeepsUnimplementedConfigCalls(t *testing.T) {
	instance, err := newRuntime(&runtimeFixture{Base: NewBase()})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(context.Background(), InitializeOptions{
		Host:    NewHost(nil, nil, nil, nil, nil, nil, nil),
		DataDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.ValidateConfig(context.Background(), nil); err == nil {
		t.Fatal("ValidateConfig() unexpectedly succeeded without Config[T]")
	}
	if err := instance.SetConfig(context.Background(), nil); err == nil {
		t.Fatal("SetConfig() unexpectedly succeeded without Config[T]")
	}
}

type nilConfigPlugin struct {
	*Base
	*Config[testConfig]
}

func (*nilConfigPlugin) Metadata() Metadata {
	return Metadata{ID: "nil-config-fixture"}
}

func TestConfigUninitializedEmbeddedHandleIsRejected(t *testing.T) {
	_, err := newRuntime(&nilConfigPlugin{Base: NewBase()})
	if err == nil || !strings.Contains(err.Error(), "未初始化") {
		t.Fatalf("newRuntime() error = %v, want uninitialized config error", err)
	}
}

var (
	_ ConfigChangeHandler[testConfig]    = (*configTestPlugin)(nil)
	_ ConfigValidator[testConfig]        = (*validatingConfigPlugin)(nil)
	_ ConfigChangeHandler[pendingConfig] = (*pendingPlugin)(nil)
	_ CommandProvider                    = (*pendingPlugin)(nil)
	_ message.Client                     = (*configTestMessages)(nil)
)
