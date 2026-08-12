//go:build windows

package native

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")

	procRegOpenKeyExW      = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW   = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey        = advapi32.NewProc("RegCloseKey")
	procOpenProcessToken   = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = advapi32.NewProc("GetTokenInformation")

	procIsDebuggerPresent       = kernel32.NewProc("IsDebuggerPresent")
	procCreateMutexW            = kernel32.NewProc("CreateMutexW")
	procLocalFree               = kernel32.NewProc("LocalFree")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procModule32FirstW          = kernel32.NewProc("Module32FirstW")
	procModule32NextW           = kernel32.NewProc("Module32NextW")
	procCloseHandle             = kernel32.NewProc("CloseHandle")

	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

const (
	hklm            = uintptr(0x80000002)
	keyRead         = uint32(0x00020019)
	tokenQuery      = uint32(0x0008)
	tokenElevation  = uint32(20)
	cryptUIForbidden = uint32(0x00000001)
	errorAlreadyExists = syscall.Errno(183)
	th32csSnapModule = uintptr(0x00000008)
)

// readMachineGuid 读取 HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid
func readMachineGuid() (string, error) {
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		hklm,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(`SOFTWARE\Microsoft\Cryptography`))),
		0, uintptr(keyRead), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return "", fmt.Errorf("RegOpenKeyExW: %d", r)
	}
	defer procRegCloseKey.Call(uintptr(h))

	namePtr := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("MachineGuid")))
	var size uint32
	r, _, _ = procRegQueryValueExW.Call(uintptr(h), namePtr, 0, 0, 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size == 0 {
		return "", fmt.Errorf("RegQueryValueExW(size): %d", r)
	}
	buf := make([]uint16, size/2+1)
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(h), namePtr, 0, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", fmt.Errorf("RegQueryValueExW: %d", r)
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf)), nil
}

// registryKeyExists 判断注册表键是否存在（用于 VM/沙箱检测）
func registryKeyExists(root uintptr, subKey string) bool {
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		root,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(subKey))),
		0, uintptr(keyRead), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return false
	}
	procRegCloseKey.Call(uintptr(h))
	return true
}

// isDebuggerPresent 反调试：IsDebuggerPresent + PEB 检测
func isDebuggerPresent() bool {
	r, _, _ := procIsDebuggerPresent.Call()
	return r != 0
}

// tokenElevation 结构（TOKEN_ELEVATION）
type tokenElevation struct {
	TokenIsElevated uint32
}

// isElevated 检测进程是否以管理员权限运行（root 风险信号）
func isElevated() bool {
	proc, err := syscall.GetCurrentProcess()
	if err != nil {
		return false
	}
	var token syscall.Token
	if err := syscall.OpenProcessToken(proc, tokenQuery, &token); err != nil {
		return false
	}
	defer token.Close()

	var elev tokenElevation
	var retLen uint32
	r, _, _ := procGetTokenInformation.Call(
		uintptr(token), uintptr(tokenElevation),
		uintptr(unsafe.Pointer(&elev)), unsafe.Sizeof(elev),
		uintptr(unsafe.Pointer(&retLen)))
	return r != 0 && elev.TokenIsElevated != 0
}

// moduleEntry32 MODULEENTRY32W
type moduleEntry32 struct {
	Size         uint32
	ModuleID     uint32
	ProcessID    uint32
	GlblcntUsage uint32
	ProccntUsage uint32
	ModBaseAddr  *byte
	ModBaseSize  uint32
	ModuleHandle syscall.Handle
	Module       [256]uint16
	ExePath      [260]uint16
}

// loadedModules 枚举当前进程已加载模块（hook 注入检测用）
func loadedModules() []string {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapModule, 0)
	if snap == ^uintptr(0) {
		return nil
	}
	defer procCloseHandle.Call(snap)

	var me moduleEntry32
	me.Size = uint32(unsafe.Sizeof(me))
	r, _, _ := procModule32FirstW.Call(snap, uintptr(unsafe.Pointer(&me)))
	if r == 0 {
		return nil
	}
	var names []string
	for {
		names = append(names, strings.ToLower(syscall.UTF16ToString(me.Module[:])))
		r, _, _ = procModule32NextW.Call(snap, uintptr(unsafe.Pointer(&me)))
		if r == 0 {
			break
		}
	}
	return names
}

// dataBlob DATA_BLOB（DPAPI）
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
}

func blobBytes(b *dataBlob) []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	return unsafe.Slice(b.pbData, int(b.cbData))
}

// protectData CryptProtectData（当前用户范围加密）
func protectData(data []byte) ([]byte, error) {
	in := newBlob(data)
	var out dataBlob
	r, _, _ := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(in)), 0, 0, 0, 0,
		uintptr(cryptUIForbidden), uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("CryptProtectData 失败")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), blobBytes(&out)...), nil
}

// unprotectData CryptUnprotectData
func unprotectData(data []byte) ([]byte, error) {
	in := newBlob(data)
	var out dataBlob
	r, _, _ := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(in)), 0, 0, 0, 0,
		uintptr(cryptUIForbidden), uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, errors.New("CryptUnprotectData 失败")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), blobBytes(&out)...), nil
}
