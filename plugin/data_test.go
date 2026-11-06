package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/golemwx/sdk"
)

func TestDataWithoutFileIODoesNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins", "data", "idle")
	instance, err := newRuntime(&runtimeFixture{Base: NewBase()})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{
		Host: NewHost(nil, nil, nil, nil, nil, nil, nil), DataDir: root,
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := instance.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := instance.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := instance.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := instance.Unload(t.Context()); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}
	assertNotExist(t, root)
}

func TestDataReadOnlyDoesNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins", "data", "reader")
	data, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	if _, err := data.ReadFile("state.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile() error = %v, want fs.ErrNotExist", err)
	}
	if _, err := data.Open("state.json", os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open() error = %v, want fs.ErrNotExist", err)
	}
	if err := data.Remove("state.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Remove() error = %v, want fs.ErrNotExist", err)
	}
	assertNotExist(t, root)
}

func TestDataFirstWriteCreatesOnlyOwnRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "plugins", "data", "writer")
	sibling := filepath.Join(base, "plugins", "data", "sibling")
	data, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	if err := data.WriteFile("state/index.json", []byte(`{"ready":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	content, err := data.ReadFile("state/index.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != `{"ready":true}` {
		t.Fatalf("ReadFile() = %q, want written content", content)
	}
	file, err := data.Open("cache/item.bin", os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := file.Write([]byte("item")); err != nil {
		t.Fatalf("file.Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file.Close() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(base, "plugins", "data"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "writer" {
		t.Fatalf("data directory entries = %v, want only the writer root", entryNames(entries))
	}
	assertNotExist(t, sibling)
}

func TestDataReadWriteSurvivesRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins", "data", "restart")
	first, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	payload := []byte("persisted")
	if err := first.WriteFile("state/history.json", payload, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	second, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	content, err := second.ReadFile("state/history.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != string(payload) {
		t.Fatalf("ReadFile() = %q, want %q", content, payload)
	}
}

func TestDataRejectsEscapingPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside.txt")
	data, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	operations := []struct {
		name string
		call func(string) error
	}{
		{name: "read", call: func(name string) error {
			_, err := data.ReadFile(name)
			return err
		}},
		{name: "open", call: func(name string) error {
			_, err := data.Open(name, os.O_RDONLY, 0)
			return err
		}},
		{name: "write", call: func(name string) error {
			return data.WriteFile(name, []byte("escape"), 0o600)
		}},
		{name: "remove", call: func(name string) error {
			return data.Remove(name)
		}},
	}
	names := []string{
		"",
		".",
		"..",
		"../outside.txt",
		`..\outside.txt`,
		"a/../../outside.txt",
		"a/..",
		"/outside.txt",
		`C:\outside.txt`,
		"c:outside.txt",
		`\\server\share\outside.txt`,
	}
	for _, operation := range operations {
		for _, name := range names {
			t.Run(operation.name+" "+name, func(t *testing.T) {
				var business *sdk.Error
				if err := operation.call(name); !errors.As(err, &business) || business.Category != sdk.CategoryInvalidArgument {
					t.Fatalf("call(%q) error = %v, want InvalidArgument", name, err)
				}
			})
		}
	}
	assertNotExist(t, root)
	assertNotExist(t, outside)
}

func TestDataRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	var links []string
	absolute := filepath.Join(root, "absolute")
	if err := os.Symlink(outside, absolute); err == nil {
		links = append(links, "absolute")
	} else if goruntime.GOOS == "windows" && createJunction(absolute, outside) == nil {
		links = append(links, "absolute")
	}
	relative := filepath.Join(root, "relative")
	if err := os.Symlink(filepath.Join("..", "outside"), relative); err == nil {
		links = append(links, "relative")
	}
	if len(links) == 0 {
		t.Skip("neither symlinks nor junctions are available")
	}

	data, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	for _, link := range links {
		content, err := data.ReadFile(filepath.Join(link, "secret.txt"))
		if err == nil {
			t.Fatalf("ReadFile(%s) = %q, want a rejected escape", link, content)
		}
		if err := data.WriteFile(filepath.Join(link, "new.txt"), []byte("escape"), 0o600); err == nil {
			t.Fatalf("WriteFile(%s) succeeded, want a rejected escape", link)
		}
	}
	assertNotExist(t, filepath.Join(outside, "new.txt"))
}

func createJunction(link, target string) error {
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J: %w: %s", err, output)
	}
	return nil
}

func TestDataRootIsBoundByInitialize(t *testing.T) {
	instanceRoot := filepath.Join(t.TempDir(), "instance")
	injectedRoot := filepath.Join(t.TempDir(), "injected")
	injected, err := newDataDir(injectedRoot)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	host := NewHost(nil, nil, nil, nil, nil, nil, injected)
	instance, err := newRuntime(&runtimeFixture{Base: NewBase()})
	if err != nil {
		t.Fatalf("newRuntime() error = %v", err)
	}
	if err := instance.Initialize(t.Context(), InitializeOptions{Host: host, DataDir: instanceRoot}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if err := host.Data.WriteFile("marker.txt", []byte("bound"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(instanceRoot, "marker.txt")); err != nil {
		t.Fatalf("instance data root was not used: %v", err)
	}
	assertNotExist(t, injectedRoot)
}

func TestDataRemoveDoesNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins", "data", "remover")
	data, err := newDataDir(root)
	if err != nil {
		t.Fatalf("newDataDir() error = %v", err)
	}
	if err := data.WriteFile("keep.txt", []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := data.WriteFile("drop.txt", []byte("drop"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := data.Remove("drop.txt"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := data.ReadFile("drop.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile(drop.txt) error = %v, want fs.ErrNotExist", err)
	}
	content, err := data.ReadFile("keep.txt")
	if err != nil || string(content) != "keep" {
		t.Fatalf("ReadFile(keep.txt) = %q, %v", content, err)
	}
	if err := data.Remove("missing.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Remove(missing.txt) error = %v, want fs.ErrNotExist", err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat(%q) error = %v, want fs.ErrNotExist", path, err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}
