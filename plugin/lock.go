package plugin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/golemwx/sdk"
)

const (
	// InstallDirEnv 覆盖 SDK 推导的插件安装根目录。
	InstallDirEnv = "GOLEM_PLUGIN_INSTALL_DIR"
	// LockPathEnv 直接指定插件实例锁文件；设置后优先于 InstallDirEnv。
	LockPathEnv = "GOLEM_PLUGIN_LOCK_PATH"
)

// InstanceLockPath 返回安装根目录下由插件 ID 唯一确定的实例锁路径。
//
// 锁文件固定位于安装根目录的 run 子目录。锁文件本身可以长期存在；
// 进程退出后由操作系统释放锁，不得以删除文件作为释放信号。
func InstanceLockPath(installDir, pluginID string) (string, error) {
	if !validPluginID(pluginID) {
		return "", sdk.NewError(sdk.CategoryInvalidArgument, "plugin.lock", "plugin ID is invalid")
	}
	if strings.TrimSpace(installDir) == "" {
		return "", sdk.NewError(sdk.CategoryInvalidArgument, "plugin.lock", "install directory is empty")
	}
	absolute, err := filepath.Abs(installDir)
	if err != nil {
		return "", sdk.NewError(sdk.CategoryInvalidArgument, "plugin.lock", "install directory is invalid")
	}
	absolute = filepath.Clean(absolute)
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	return filepath.Join(absolute, "run", "plugin-"+pluginID+".lock"), nil
}

func configuredLockPath(pluginID string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(LockPathEnv)); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", sdk.NewError(sdk.CategoryInvalidArgument, "plugin.lock", "lock path is invalid")
		}
		return filepath.Clean(absolute), nil
	}
	installDir := strings.TrimSpace(os.Getenv(InstallDirEnv))
	if installDir == "" {
		executable, err := os.Executable()
		if err != nil {
			return "", sdk.NewError(sdk.CategoryInternal, "plugin.lock", "plugin executable path is unavailable")
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			return "", sdk.NewError(sdk.CategoryInternal, "plugin.lock", "plugin executable path is invalid")
		}
		installDir = filepath.Dir(filepath.Clean(executable))
		if strings.EqualFold(filepath.Base(installDir), "plugins") {
			installDir = filepath.Dir(installDir)
		}
	}
	return InstanceLockPath(installDir, pluginID)
}

func acquireInstanceLock(pluginID string) (*flock.Flock, error) {
	path, err := configuredLockPath(pluginID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.lock", "instance lock directory could not be created")
	}
	instance := flock.New(path)
	locked, err := instance.TryLock()
	if err != nil {
		_ = instance.Close()
		return nil, sdk.NewError(sdk.CategoryInternal, "plugin.lock", "instance lock could not be acquired")
	}
	if !locked {
		_ = instance.Close()
		return nil, sdk.NewError(sdk.CategoryFailedPrecondition, "plugin.lock", "another plugin instance is already running")
	}
	return instance, nil
}

func closeInstanceLock(instance *flock.Flock) {
	if instance == nil {
		return
	}
	_ = instance.Close()
}
