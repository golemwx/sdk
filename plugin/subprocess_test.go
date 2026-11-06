package plugin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golemwx/sdk"
	"github.com/golemwx/sdk/command"
	"github.com/golemwx/sdk/event"
)

const subprocessFixtureID = "subprocess-fixture"

type subprocessAccount struct {
	calls atomic.Int32
}

func (a *subprocessAccount) Current(context.Context) (*sdk.Account, error) {
	a.calls.Add(1)
	return &sdk.Account{Username: "wxid_owner", Nickname: "Owner", Online: true}, nil
}

type subprocessConfig struct {
	mu   sync.Mutex
	data []byte
}

func (c *subprocessConfig) Read(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...), nil
}

func (c *subprocessConfig) Save(_ context.Context, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = append([]byte(nil), data...)
	return nil
}

func (c *subprocessConfig) contents() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.data)
}

type subprocessStart struct {
	client *Client
	err    error
}

func TestSubprocessSingleInstanceLifecycleAndReload(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "lifecycle.log")

	start := func() (*Client, error) {
		return startSubprocessFixture(t, binary, installDir, markerPath, "normal")
	}

	results := make(chan subprocessStart, 2)
	ready := make(chan struct{})
	var starters sync.WaitGroup
	for range 2 {
		starters.Add(1)
		go func() {
			defer starters.Done()
			<-ready
			client, err := start()
			results <- subprocessStart{client: client, err: err}
		}()
	}
	close(ready)
	starters.Wait()
	close(results)

	var winner *Client
	for result := range results {
		if result.err != nil {
			continue
		}
		if winner != nil {
			_ = result.client.Close()
			t.Fatal("two processes with the same plugin ID both started")
		}
		winner = result.client
	}
	if winner == nil {
		t.Fatal("no process acquired the plugin instance lock")
	}
	t.Cleanup(func() { _ = winner.Close() })

	descriptor := winner.Descriptor()
	if descriptor.Metadata.ID != subprocessFixtureID || len(descriptor.Subscriptions) != 1 {
		t.Fatalf("Descriptor() = %+v", descriptor)
	}
	lockPath := filepath.Join(installDir, "run", "plugin-"+subprocessFixtureID+".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("instance lock file is missing: %v", err)
	}
	dataRoot := filepath.Join(installDir, "plugins", "data", subprocessFixtureID)
	if _, err := os.Stat(dataRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plugin data directory was touched: %v", err)
	}

	if err := winner.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := winner.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForMarker(t, markerPath, "host-current")
	waitForMarker(t, markerPath, "cycle-active")
	if got := markerCount(t, markerPath, "load"); got != 1 {
		t.Fatalf("load marker count = %d, want 1", got)
	}
	if err := winner.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForMarker(t, markerPath, "stop")
	waitForMarker(t, markerPath, "cycle-canceled")
	waitForMarker(t, markerPath, "host-disabled")

	if err := winner.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
	if !winner.Exited() {
		t.Fatal("Unload() returned without confirming process exit")
	}
	if got := markerCount(t, markerPath, "unload"); got != 1 {
		t.Fatalf("unload marker count = %d, want 1", got)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should remain after process exit: %v", err)
	}

	second, err := start()
	if err != nil {
		t.Fatalf("Start() after unload error = %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.Load(t.Context()); err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if err := second.Start(t.Context()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if err := second.Unload(t.Context()); err != nil {
		t.Fatalf("second Unload() error = %v", err)
	}
	if !second.Exited() {
		t.Fatal("second Unload() returned without confirming process exit")
	}
	if got := markerCount(t, markerPath, "load"); got != 2 {
		t.Fatalf("load marker count after reload = %d, want 2", got)
	}
	if got := markerCount(t, markerPath, "stop"); got != 2 {
		t.Fatalf("stop marker count after direct unload = %d, want 2", got)
	}
	if got := markerCount(t, markerPath, "unload"); got != 2 {
		t.Fatalf("unload marker count after reload = %d, want 2", got)
	}
}

func TestSubprocessStartFailureStillCleansUp(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "start-failure.log")
	client, err := startSubprocessFixture(t, binary, installDir, markerPath, "start-fail")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	err = client.Start(t.Context())
	var business *sdk.Error
	if !errors.As(err, &business) || business.Category != sdk.CategoryInternal {
		t.Fatalf("Start() error = %v, want business Internal", err)
	}
	waitForMarker(t, markerPath, "start-fail")
	if got := markerCount(t, markerPath, "stop"); got != 0 {
		t.Fatalf("OnStop was called after failed Start: count = %d", got)
	}
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() after failed Start error = %v", err)
	}
	if !client.Exited() {
		t.Fatal("failed-start process was not confirmed exited")
	}
	if got := markerCount(t, markerPath, "unload"); got != 1 {
		t.Fatalf("unload marker count = %d, want 1", got)
	}
}

func TestSubprocessStopWaitsForHandlerIgnoringCancellation(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "ignored-cancel.log")
	client, err := startSubprocessFixture(t, binary, installDir, markerPath, "ignore-cancel")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	eventDone := make(chan error, 1)
	go func() {
		_, eventErr := client.HandleEvent(context.Background(), event.Event{
			Topic: event.Message, OccurredAt: time.Now(),
		})
		eventDone <- eventErr
	}()
	waitForMarker(t, markerPath, "event-start")

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	err = client.Stop(stopCtx)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("Stop() returned after %v, before the handler exited", elapsed)
	}
	if err := <-eventDone; err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	waitForMarker(t, markerPath, "event-end")
	waitForMarker(t, markerPath, "cycle-canceled")
	waitForMarker(t, markerPath, "host-disabled")
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
	if !client.Exited() {
		t.Fatal("Unload() returned without confirming process exit")
	}
}

func TestSubprocessHostDisconnectInvalidatesInstance(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "disconnect.log")
	client, err := startSubprocessFixture(t, binary, installDir, markerPath, "normal")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	client.closeHostServer()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, callErr := client.HandleEvent(context.Background(), event.Event{
			Topic: event.Message, OccurredAt: time.Now(),
		})
		var business *sdk.Error
		if errors.As(callErr, &business) && business.Category == sdk.CategoryFailedPrecondition {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disconnected instance kept accepting events; last error = %v", callErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitForMarker(t, markerPath, "cycle-canceled")
	waitForMarker(t, markerPath, "host-disabled")
	if err := client.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() after disconnect error = %v", err)
	}
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() after disconnect error = %v", err)
	}
	if !client.Exited() {
		t.Fatal("Unload() returned without confirming process exit")
	}
}

func TestSubprocessConfigUpdateKeepsProcessAlive(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "config-update.log")
	client, err := startSubprocessFixture(t, binary, installDir, markerPath, "normal")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pid := client.PID()
	if pid == "" {
		t.Fatal("PID() is empty")
	}
	if err := client.SetConfig(t.Context(), []byte("enabled = false\n")); err != nil {
		t.Fatalf("SetConfig() error = %v", err)
	}
	if got := client.PID(); got != pid {
		t.Fatalf("PID changed after config update: %q -> %q", pid, got)
	}
	if client.Exited() {
		t.Fatal("config update restarted or exited the plugin process")
	}
	waitForMarker(t, markerPath, "config-changed")
	if got := markerCount(t, markerPath, "start"); got != 1 {
		t.Fatalf("start marker count = %d, want 1", got)
	}
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestSubprocessOwnerConfigUpdateKeepsProcessAlive(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "config-owner-update.log")
	config := &subprocessConfig{}
	client, err := startSubprocessFixtureWithConfig(t, binary, installDir, markerPath, "normal", config)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := client.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	pid := client.PID()
	if pid == "" {
		t.Fatal("PID() is empty")
	}
	commandMessage := ownerMessage()
	commandMessage.Timestamp = time.Now()
	invocation := command.Invocation{
		Tokens:  []string{"config", "--enabled=false"},
		Owner:   true,
		Message: commandMessage,
	}
	if err := client.HandleCommand(t.Context(), invocation); err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}
	if got := client.PID(); got != pid {
		t.Fatalf("PID changed after owner config update: %q -> %q", pid, got)
	}
	if client.Exited() {
		t.Fatal("owner config update restarted or exited the plugin process")
	}
	waitForMarker(t, markerPath, "config-changed")
	if got := markerCount(t, markerPath, "config-mutate"); got != 1 {
		t.Fatalf("config mutate marker count = %d, want 1", got)
	}
	if got := markerCount(t, markerPath, "start"); got != 1 {
		t.Fatalf("start marker count = %d, want 1", got)
	}
	if saved := config.contents(); !strings.Contains(saved, "enabled = false") {
		t.Fatalf("saved config = %q, want enabled = false", saved)
	}

	invocation.Owner = false
	if err := client.HandleCommand(t.Context(), invocation); err == nil {
		t.Fatal("non-owner config command was accepted")
	}
	if got := markerCount(t, markerPath, "config-mutate"); got != 1 {
		t.Fatalf("non-owner command mutated config: count = %d", got)
	}
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func buildSubprocessFixture(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	sourceDir := filepath.Join(filepath.Dir(sourceFile), "testdata", "subprocess")
	suffix := ""
	if goruntime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(t.TempDir(), "subprocess-fixture"+suffix)
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Dir = sourceDir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build subprocess fixture: %v\n%s", err, output)
	}
	return binary
}

func startSubprocessFixture(t *testing.T, binary, installDir, markerPath, mode string) (*Client, error) {
	return startSubprocessFixtureWithConfig(t, binary, installDir, markerPath, mode, nil)
}

func startSubprocessFixtureWithConfig(t *testing.T, binary, installDir, markerPath, mode string, config ConfigClient) (*Client, error) {
	return startSubprocessFixtureWithOptions(t, binary, installDir, markerPath, mode, config, nil)
}

func startSubprocessFixtureWithLogger(t *testing.T, binary, installDir, markerPath, mode string, logger *slog.Logger) (*Client, error) {
	return startSubprocessFixtureWithOptions(t, binary, installDir, markerPath, mode, nil, logger)
}

func startSubprocessFixtureWithOptions(
	t *testing.T,
	binary, installDir, markerPath, mode string,
	config ConfigClient,
	logger *slog.Logger,
) (*Client, error) {
	t.Helper()
	command := exec.Command(binary)
	command.Env = append(os.Environ(),
		InstallDirEnv+"="+installDir,
		"GOLEM_FIXTURE_MARKER="+markerPath,
		"GOLEM_FIXTURE_MODE="+mode,
	)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	return Start(ctx, ClientOptions{
		Command:      command,
		PluginID:     subprocessFixtureID,
		Host:         NewHost(&subprocessAccount{}, &configTestMessages{}, nil, nil, nil, nil, nil),
		ConfigClient: config,
		DataRoot:     filepath.Join(installDir, "plugins", "data", subprocessFixtureID),
		Logger:       logger,
	})
}

func waitForMarker(t *testing.T, path, value string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if markerCount(t, path, value) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker %q was not written to %s; got:\n%s", value, path, markerContents(path))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func markerCount(t *testing.T, path, value string) int {
	t.Helper()
	count := 0
	for _, line := range strings.Split(markerContents(path), "\n") {
		if line == value {
			count++
		}
	}
	return count
}

func markerContents(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(contents)
}
