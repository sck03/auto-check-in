//go:build windows

package security

import (
	"fmt"
	"path/filepath"
	"syscall"
)

func LockDirectory(dir string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(filepath.Join(dir, "instance.lock"))
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("该数据目录已被另一个进程使用，或不可写：%w", err)
	}
	return func() { _ = syscall.CloseHandle(handle) }, nil
}
