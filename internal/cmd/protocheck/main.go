// Command protocheck 在临时目录重复生成 Proto 并核对字节级差异
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// bufVersion Proto 格式检查与生成使用的固定 Buf 版本
const bufVersion = "v1.75.0"

// main 在临时目录重新生成并核对全部生成文件的一致性
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 在返回错误之前释放临时目录，避免 os.Exit 跳过 defer
func run() error {
	tempDir, err := os.MkdirTemp("", "golem-sdk-proto-check-")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	command := exec.Command(
		"go",
		"run",
		"github.com/bufbuild/buf/cmd/buf@"+bufVersion,
		"generate",
		"--output",
		filepath.ToSlash(tempDir),
	)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("generate protocol files: %w", err)
	}

	expectedRoot := filepath.Join("internal", "wire")
	actualRoot := filepath.Join(tempDir, "internal", "wire")
	expected, err := generatedFiles(expectedRoot)
	if err != nil {
		return fmt.Errorf("read checked-in generated files: %w", err)
	}
	actual, err := generatedFiles(actualRoot)
	if err != nil {
		return fmt.Errorf("read regenerated files: %w", err)
	}

	var differences []string
	for name, expectedData := range expected {
		actualData, ok := actual[name]
		if !ok {
			differences = append(differences, "missing regenerated file: "+name)
			continue
		}
		if !bytes.Equal(expectedData, actualData) {
			differences = append(differences, "generated file differs: "+name)
		}
	}
	for name := range actual {
		if _, ok := expected[name]; !ok {
			differences = append(differences, "unexpected regenerated file: "+name)
		}
	}

	if len(differences) != 0 {
		sort.Strings(differences)
		return fmt.Errorf("generated output is not reproducible: %s", strings.Join(differences, "; "))
	}

	fmt.Println("generated output is byte-identical")
	return nil
}

// generatedFiles 读取指定目录内的 Proto 生成文件并按相对路径索引
func generatedFiles(root string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pb.go") {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no generated protocol files found in %s", root)
	}

	return result, nil
}
