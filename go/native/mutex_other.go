//go:build !windows

package native

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// acquireMutex 防多开：文件锁（flock）。
func acquireMutex(appID string) (bool, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "aohoyo", "locks", sanitizeKey(appID)+".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil // 已有实例
		}
		return false, err
	}
	// 句柄进程生命周期内保持
	mutexFiles = append(mutexFiles, f)
	return true, nil
}

var mutexFiles []*os.File
