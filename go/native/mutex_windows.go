//go:build windows

package native

import (
	"strings"
	"syscall"
	"unsafe"
)

// mutexHandles 进程生命周期内保持互斥句柄（句柄被 GC 不影响，但显式持有更稳妥）
var mutexHandles []uintptr

// acquireMutex 防多开：Global 命名空间互斥体。
func acquireMutex(appID string) (bool, error) {
	name := "Global\\aohoyo_" + strings.NewReplacer("\\", "_", "/", "_").Replace(appID)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))))
	if h == 0 {
		if e, ok := err.(syscall.Errno); ok {
			return false, e
		}
		return false, syscall.EINVAL
	}
	if e, ok := err.(syscall.Errno); ok && e == errorAlreadyExists {
		return false, nil // 已有实例
	}
	mutexHandles = append(mutexHandles, h)
	return true, nil
}
