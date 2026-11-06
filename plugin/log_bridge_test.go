package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	hclog "github.com/hashicorp/go-hclog"
)

type capturedRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

type captureState struct {
	mu      sync.Mutex
	records []capturedRecord
}

type captureHandler struct {
	state  *captureState
	attrs  []slog.Attr
	prefix string
}

func newCaptureHandler() *captureHandler {
	return &captureHandler{state: &captureState{}}
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any)
	for _, attr := range h.attrs {
		addCaptureAttr(attrs, h.prefix, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addCaptureAttr(attrs, h.prefix, attr)
		return true
	})
	h.state.mu.Lock()
	h.state.records = append(h.state.records, capturedRecord{
		level:   record.Level,
		message: record.Message,
		attrs:   attrs,
	})
	h.state.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := *h
	next.prefix += name + "."
	return &next
}

func (h *captureHandler) snapshot() []capturedRecord {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	records := make([]capturedRecord, len(h.state.records))
	for index, record := range h.state.records {
		attrs := make(map[string]any, len(record.attrs))
		for key, value := range record.attrs {
			attrs[key] = value
		}
		records[index] = capturedRecord{level: record.level, message: record.message, attrs: attrs}
	}
	return records
}

func addCaptureAttr(attrs map[string]any, prefix string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		nextPrefix := prefix
		if attr.Key != "" {
			nextPrefix += attr.Key + "."
		}
		for _, child := range attr.Value.Group() {
			addCaptureAttr(attrs, nextPrefix, child)
		}
		return
	}
	if attr.Key == "" {
		return
	}
	attrs[prefix+attr.Key] = attr.Value.Any()
}

func TestSlogToHclogBridgePreservesLevelsAttrsAndGroups(t *testing.T) {
	var output bytes.Buffer
	hclogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		Output:     &output,
		JSONFormat: true,
	})
	identity := newLogIdentity("fixture", "Fixture")
	logger := slog.New(&identityHandler{
		next:     &hclogSlogHandler{logger: hclogger},
		identity: identity,
	})

	logger.Debug("debug message", "answer", 42)
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message", "code", 7)
	logger.With("component", "bridge").WithGroup("request").Info("grouped message", "id", 9)
	logger.Info("identity attempt",
		LogFieldPluginID, "spoofed",
		LogFieldPluginName, "spoofed",
		"timestamp", "2000-01-01T00:00:00Z",
	)
	logger.WithGroup("nested").Info("grouped identity attempt",
		LogFieldPluginID, "spoofed",
		LogFieldPluginName, "spoofed",
	)
	logger.Info("nested group",
		slog.Group("a",
			slog.Int("b", 1),
			slog.Group("c", slog.String("d", "e")),
		),
	)
	logger.WithGroup("ordered").With("value", 2).Info("ordered group")

	records := decodeHclogJSON(t, output.Bytes())
	if len(records) != 9 {
		t.Fatalf("log line count = %d, want 9\n%s", len(records), output.String())
	}
	assertHclogLevel(t, records["debug message"], "debug")
	assertHclogLevel(t, records["info message"], "info")
	assertHclogLevel(t, records["warn message"], "warn")
	assertHclogLevel(t, records["error message"], "error")
	assertHclogLevel(t, records["grouped message"], "info")
	assertHclogLevel(t, records["identity attempt"], "info")
	assertHclogLevel(t, records["grouped identity attempt"], "info")
	assertHclogLevel(t, records["nested group"], "info")
	assertHclogLevel(t, records["ordered group"], "info")

	grouped := records["grouped message"]
	if grouped["component"] != "bridge" {
		t.Fatalf("grouped component = %v, want bridge", grouped["component"])
	}
	if grouped["request.id"] != float64(9) {
		t.Fatalf("grouped request.id = %v, want 9", grouped["request.id"])
	}
	if grouped[LogFieldPluginID] != "fixture" || grouped[LogFieldPluginName] != "Fixture" {
		t.Fatalf("grouped identity = %v/%v", grouped[LogFieldPluginID], grouped[LogFieldPluginName])
	}

	attempt := records["identity attempt"]
	if attempt[LogFieldPluginID] != "fixture" || attempt[LogFieldPluginName] != "Fixture" {
		t.Fatalf("trusted identity = %v/%v", attempt[LogFieldPluginID], attempt[LogFieldPluginName])
	}
	if _, exists := attempt["timestamp"]; exists {
		t.Fatal("plugin-provided timestamp was forwarded to hclog")
	}
	if _, ok := attempt["@timestamp"].(string); !ok {
		t.Fatalf("@timestamp = %#v, want string", attempt["@timestamp"])
	}
	nested := records["grouped identity attempt"]
	if nested[LogFieldPluginID] != "fixture" || nested[LogFieldPluginName] != "Fixture" {
		t.Fatalf("nested identity = %v/%v", nested[LogFieldPluginID], nested[LogFieldPluginName])
	}
	if _, exists := nested["nested."+LogFieldPluginID]; exists {
		t.Fatal("grouped identity override was preserved")
	}
	deep := records["nested group"]
	if deep["a.b"] != float64(1) || deep["a.c.d"] != "e" {
		t.Fatalf("nested group fields = %v", deep)
	}
	ordered := records["ordered group"]
	if ordered["ordered.value"] != float64(2) {
		t.Fatalf("ordered group fields = %v", ordered)
	}
	if strings.ContainsRune(output.String(), '\x1b') {
		t.Fatal("plugin log output contains ANSI escape sequences")
	}
}

func TestSlogBridgeIdentityAppliesToExistingLogger(t *testing.T) {
	var output bytes.Buffer
	hclogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		Output:     &output,
		JSONFormat: true,
	})
	identity := newLogIdentity("", "")
	logger := slog.New(&identityHandler{
		next:     &hclogSlogHandler{logger: hclogger},
		identity: identity,
	})
	derived := logger.With("component", "early").WithGroup("late")

	identity.Set("fixture", "Fixture")
	derived.Info("late identity", "value", "ok")

	record := decodeHclogJSON(t, output.Bytes())["late identity"]
	if record[LogFieldPluginID] != "fixture" || record[LogFieldPluginName] != "Fixture" {
		t.Fatalf("late identity = %v/%v", record[LogFieldPluginID], record[LogFieldPluginName])
	}
	if record["component"] != "early" || record["late.value"] != "ok" {
		t.Fatalf("derived attrs = %v", record)
	}
}

func TestSlogBridgeHonorsHclogLevel(t *testing.T) {
	var output bytes.Buffer
	hclogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Info,
		Output:     &output,
		JSONFormat: true,
	})
	logger := slog.New(&identityHandler{
		next:     &hclogSlogHandler{logger: hclogger},
		identity: newLogIdentity("fixture", ""),
	})

	logger.Debug("hidden debug")
	logger.Info("visible info")

	records := decodeHclogJSON(t, output.Bytes())
	if _, exists := records["hidden debug"]; exists {
		t.Fatal("Debug record passed an Info-level hclog logger")
	}
	if _, exists := records["visible info"]; !exists {
		t.Fatal("Info record was filtered out")
	}
}

func TestHostLogAdapterMapsLevelsAndTrustedIdentity(t *testing.T) {
	capture := newCaptureHandler()
	identity := newLogIdentity("fixture", "Fixture")
	hostLogger := newHclogSlogLogger(slog.New(&identityHandler{
		next:     capture,
		identity: identity,
	}))

	hostLogger.Trace("trace message")
	hostLogger.Debug("debug message", "answer", 42)
	hostLogger.Info("info message")
	hostLogger.Warn("warn message")
	hostLogger.Error("error message", "code", 7)
	hostLogger.With("component", "host").Named("plugin.exe").Info("with attrs")
	hostLogger.Info("spoof attempt",
		LogFieldPluginID, "spoofed",
		LogFieldPluginName, "spoofed",
		"timestamp", "2000-01-01T00:00:00Z",
	)

	records := capture.snapshot()
	if len(records) != 7 {
		t.Fatalf("record count = %d, want 7: %+v", len(records), records)
	}
	assertCaptureLevel(t, records, "trace message", slogLevelTrace)
	assertCaptureLevel(t, records, "debug message", slog.LevelDebug)
	assertCaptureLevel(t, records, "info message", slog.LevelInfo)
	assertCaptureLevel(t, records, "warn message", slog.LevelWarn)
	assertCaptureLevel(t, records, "error message", slog.LevelError)
	assertCaptureLevel(t, records, "with attrs", slog.LevelInfo)
	assertCaptureLevel(t, records, "spoof attempt", slog.LevelInfo)

	spoof := findCaptureRecord(t, records, "spoof attempt")
	if spoof.attrs[LogFieldPluginID] != "fixture" || spoof.attrs[LogFieldPluginName] != "Fixture" {
		t.Fatalf("trusted identity = %v/%v", spoof.attrs[LogFieldPluginID], spoof.attrs[LogFieldPluginName])
	}
	if _, exists := spoof.attrs["timestamp"]; exists {
		t.Fatal("host adapter forwarded duplicate timestamp")
	}
	withAttrs := findCaptureRecord(t, records, "with attrs")
	if withAttrs.attrs["component"] != "host" {
		t.Fatalf("with attrs = %v", withAttrs.attrs)
	}
	if hostLogger.Name() != "" || hostLogger.Named("plugin.exe").Name() != "plugin.exe" {
		t.Fatal("hclog logger name handling is incorrect")
	}
}

func TestSubprocessLogBridgeDeliversStructuredLogsToHost(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "log-bridge.log")
	capture := newCaptureHandler()
	client, err := startSubprocessFixtureWithLogger(t, binary, installDir, markerPath, "normal", slog.New(capture))
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

	for _, message := range []string{
		"fixture debug",
		"fixture info",
		"fixture warning",
		"fixture error",
		"fixture grouped",
		"fixture identity attempt",
		"fixture plain stderr",
	} {
		waitForCaptureMessage(t, capture, message)
	}

	records := capture.snapshot()
	assertCaptureLevel(t, records, "fixture debug", slog.LevelDebug)
	assertCaptureLevel(t, records, "fixture info", slog.LevelInfo)
	assertCaptureLevel(t, records, "fixture warning", slog.LevelWarn)
	assertCaptureLevel(t, records, "fixture error", slog.LevelError)
	assertCaptureLevel(t, records, "fixture plain stderr", slog.LevelWarn)

	for _, message := range []string{
		"fixture debug",
		"fixture info",
		"fixture warning",
		"fixture error",
		"fixture grouped",
		"fixture identity attempt",
		"fixture plain stderr",
	} {
		if count := captureMessageCount(records, message); count != 1 {
			t.Fatalf("%s delivered %d times, want once", message, count)
		}
		record := findCaptureRecord(t, records, message)
		if record.attrs[LogFieldPluginID] != subprocessFixtureID {
			t.Fatalf("%s plugin_id = %v, want %s", message, record.attrs[LogFieldPluginID], subprocessFixtureID)
		}
		if record.attrs[LogFieldPluginName] != "子进程夹具" {
			t.Fatalf("%s plugin_name = %v", message, record.attrs[LogFieldPluginName])
		}
		if _, exists := record.attrs["timestamp"]; exists {
			t.Fatalf("%s contains a duplicate timestamp", message)
		}
	}

	grouped := findCaptureRecord(t, records, "fixture grouped")
	if grouped.attrs["component"] != "fixture" || grouped.attrs["outer.key"] != "value" {
		t.Fatalf("grouped attrs = %v", grouped.attrs)
	}
	attempt := findCaptureRecord(t, records, "fixture identity attempt")
	if attempt.attrs[LogFieldPluginID] != subprocessFixtureID ||
		attempt.attrs[LogFieldPluginName] != "子进程夹具" {
		t.Fatalf("plugin override reached host: %v", attempt.attrs)
	}
	plain := findCaptureRecord(t, records, "fixture plain stderr")
	if plain.attrs["stream"] != "stderr" {
		t.Fatalf("plain stderr stream attr = %v", plain.attrs["stream"])
	}
	for _, record := range records {
		if record.attrs["stream"] == "stdout" {
			t.Fatalf("plugin polluted handshake stdout: %+v", record)
		}
	}

	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func TestSubprocessStartupFailureLogReachesHost(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	capture := newCaptureHandler()
	_, err := startSubprocessFixtureWithLogger(
		t, binary, installDir, filepath.Join(t.TempDir(), "metadata-failure.log"), "metadata-fail", slog.New(capture),
	)
	if err == nil {
		t.Fatal("Start() accepted invalid metadata")
	}

	waitForCaptureMessage(t, capture, "fixture pre-handshake plain stderr")
	plain := findCaptureRecord(t, capture.snapshot(), "fixture pre-handshake plain stderr")
	if plain.level != slog.LevelDebug {
		t.Fatalf("pre-handshake plain stderr level = %v, want debug", plain.level)
	}
	if plain.attrs[LogFieldPluginID] != subprocessFixtureID {
		t.Fatalf("pre-handshake plugin_id = %v", plain.attrs[LogFieldPluginID])
	}

	waitForCaptureMessage(t, capture, "golem plugin startup failed")
	record := findCaptureRecord(t, capture.snapshot(), "golem plugin startup failed")
	if record.level != slog.LevelError {
		t.Fatalf("startup failure level = %v, want error", record.level)
	}
	if record.attrs[LogFieldPluginID] != subprocessFixtureID {
		t.Fatalf("startup failure plugin_id = %v, want %s", record.attrs[LogFieldPluginID], subprocessFixtureID)
	}
	if _, exists := record.attrs[LogFieldPluginName]; exists {
		t.Fatal("startup failure included an unverified plugin name")
	}
}

func TestSubprocessOnStartFailureLogReachesHost(t *testing.T) {
	binary := buildSubprocessFixture(t)
	installDir := t.TempDir()
	capture := newCaptureHandler()
	client, err := startSubprocessFixtureWithLogger(
		t, binary, installDir, filepath.Join(t.TempDir(), "start-failure.log"), "start-fail", slog.New(capture),
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := client.Start(t.Context()); err == nil {
		t.Fatal("Start() accepted requested failure")
	}

	waitForCaptureMessage(t, capture, "fixture start failure")
	record := findCaptureRecord(t, capture.snapshot(), "fixture start failure")
	if record.level != slog.LevelError {
		t.Fatalf("OnStart failure level = %v, want error", record.level)
	}
	if record.attrs[LogFieldPluginID] != subprocessFixtureID ||
		record.attrs[LogFieldPluginName] != "子进程夹具" {
		t.Fatalf("OnStart failure identity = %v", record.attrs)
	}
	if err := client.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
}

func decodeHclogJSON(t *testing.T, data []byte) map[string]map[string]any {
	t.Helper()
	records := make(map[string]map[string]any)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode hclog JSON %q: %v", line, err)
		}
		message, _ := record["@message"].(string)
		records[message] = record
	}
	return records
}

func assertHclogLevel(t *testing.T, record map[string]any, want string) {
	t.Helper()
	if record == nil {
		t.Fatal("hclog record is missing")
	}
	if got := record["@level"]; got != want {
		t.Fatalf("@level = %v, want %s", got, want)
	}
}

func assertCaptureLevel(t *testing.T, records []capturedRecord, message string, want slog.Level) {
	t.Helper()
	record := findCaptureRecord(t, records, message)
	if record.level != want {
		t.Fatalf("%s level = %v, want %v", message, record.level, want)
	}
}

func findCaptureRecord(t *testing.T, records []capturedRecord, message string) capturedRecord {
	t.Helper()
	for _, record := range records {
		if record.message == message {
			return record
		}
	}
	t.Fatalf("captured message %q is missing", message)
	return capturedRecord{}
}

func captureMessageCount(records []capturedRecord, message string) int {
	count := 0
	for _, record := range records {
		if record.message == message {
			count++
		}
	}
	return count
}

func waitForCaptureMessage(t *testing.T, capture *captureHandler, message string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, record := range capture.snapshot() {
			if record.message == message {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("captured message %q was not received; got %+v", message, capture.snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
