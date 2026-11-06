package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/golemwx/sdk"
)

// Data 是插件自己的惰性数据目录句柄。
//
// 文件名必须是相对路径且不能逃逸插件根目录。只读访问不存在的目录返回
// fs.ErrNotExist；Open 带创建/写标志和 WriteFile 才创建目录。
type Data interface {
	Open(name string, flag int, perm fs.FileMode) (*os.File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	Remove(name string) error
}

const dataDirectoryMode fs.FileMode = 0o700

// dataDir 把一个实例的根路径包装成受限于该目录的文件 API。
//
// 根路径在构造时只做规范化和绝对化，不读取也不创建目录。所有文件操作
// 通过 os.Root 执行，从而拒绝绝对路径、.. 以及指向根目录外的符号链接。
type dataDir struct {
	root string
}

func newDataDir(root string) (*dataDir, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.data", "data root is empty")
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, sdk.NewError(sdk.CategoryInvalidArgument, "plugin.data", "data root is invalid")
	}
	return &dataDir{root: filepath.Clean(absolute)}, nil
}

func (d *dataDir) Open(name string, flag int, perm fs.FileMode) (*os.File, error) {
	const operation = "plugin.data.open"
	relative, err := dataPath(operation, name)
	if err != nil {
		return nil, err
	}
	create := createsData(flag)
	root, err := d.openRoot(create)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if create {
		if err := makeDataParent(root, relative); err != nil {
			return nil, err
		}
	}
	return root.OpenFile(relative, flag, perm)
}

func (d *dataDir) ReadFile(name string) ([]byte, error) {
	relative, err := dataPath("plugin.data.read", name)
	if err != nil {
		return nil, err
	}
	root, err := d.openRoot(false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(relative)
}

func (d *dataDir) WriteFile(name string, data []byte, perm fs.FileMode) error {
	relative, err := dataPath("plugin.data.write", name)
	if err != nil {
		return err
	}
	root, err := d.openRoot(true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := makeDataParent(root, relative); err != nil {
		return err
	}
	file, err := root.OpenFile(relative, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}

func (d *dataDir) Remove(name string) error {
	relative, err := dataPath("plugin.data.remove", name)
	if err != nil {
		return err
	}
	root, err := d.openRoot(false)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.Remove(relative)
}

// openRoot 只在创建/写操作时创建缺失的根目录；只读操作保持惰性。
func (d *dataDir) openRoot(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(d.root, dataDirectoryMode); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(d.root)
}

func makeDataParent(root *os.Root, relative string) error {
	parent := filepath.Dir(relative)
	if parent == "." {
		return nil
	}
	return root.MkdirAll(parent, dataDirectoryMode)
}

// dataPath 校验调用方提供的名称，并返回当前平台的本地相对路径。
//
// 反斜杠在所有平台都按分隔符处理，避免同一名称在 Windows 与其他平台
// 产生不同边界。任何盘符、UNC 前缀、绝对路径或 .. 组件都被拒绝。
func dataPath(operation, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", invalidDataPath(operation)
	}
	slashed := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(slashed, "/") {
		return "", invalidDataPath(operation)
	}
	if len(slashed) >= 2 && isDriveLetter(slashed[0]) && slashed[1] == ':' {
		return "", invalidDataPath(operation)
	}
	for _, component := range strings.Split(slashed, "/") {
		if component == ".." {
			return "", invalidDataPath(operation)
		}
	}
	local := filepath.FromSlash(slashed)
	if filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return "", invalidDataPath(operation)
	}
	cleaned := filepath.Clean(local)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", invalidDataPath(operation)
	}
	return cleaned, nil
}

func invalidDataPath(operation string) error {
	return sdk.NewError(sdk.CategoryInvalidArgument, operation, "data path must be a relative path inside the plugin root")
}

func isDriveLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func createsData(flag int) bool {
	return flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0
}
