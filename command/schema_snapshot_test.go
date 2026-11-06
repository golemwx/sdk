package command

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateSchemaSnapshot = flag.Bool("update-schema-snapshot", false, "更新命令 schema 快照")

// TestSchemaSnapshot 固定结构体命令生成的完整 schema，避免参数描述悄然漂移。
func TestSchemaSnapshot(t *testing.T) {
	set := newTestSet(t)
	data, err := json.MarshalIndent(set.Schema(), "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	data = append(data, '\n')
	path := filepath.Join("testdata", "schema.golden.json")
	if *updateSchemaSnapshot {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != string(want) {
		t.Fatalf("schema snapshot mismatch\n--- got ---\n%s\n--- want ---\n%s", data, want)
	}
}
