package main

import (
	"os"
	"testing"
)

// TestGenerationFailureCleansTempDir 验证生成命令无法启动时仍删除已创建的临时目录
func TestGenerationFailureCleansTempDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("TMP", root)
	t.Setenv("TEMP", root)
	t.Setenv("PATH", t.TempDir())
	if err := run(); err == nil {
		t.Fatal("missing generation command unexpectedly succeeded")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("generation failure leaked %d temporary directories", len(entries))
	}
}

// TestEmptyGeneratedOutputRejected 验证空生成目录不能与另一空目录构成伪通过
func TestEmptyGeneratedOutputRejected(t *testing.T) {
	if _, err := generatedFiles(t.TempDir()); err == nil {
		t.Fatal("empty generated output was accepted")
	}
}
