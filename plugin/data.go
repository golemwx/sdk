package plugin

import (
	"io/fs"
	"os"
)

// Data 是插件自己的惰性数据目录句柄。
//
// 文件名必须是相对路径且不能逃逸插件根目录。只读访问不存在的目录返回
// fs.ErrNotExist；Open 带创建/写标志和 WriteFile 才创建目录。具体安全
// 实现由 R06 交付。
type Data interface {
	Open(name string, flag int, perm fs.FileMode) (*os.File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	Remove(name string) error
}
