//go:build windows

package native

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// machineFingerprint Windows 指纹 v4：MachineGuid + hostname + 主板/BIOS 序列号 + CPU
// Identifier + 显卡 DriverDesc 列表 + 物理内存字节——全部注册表/系统 API 直读（毫秒级，
// 不再走 PowerShell/WMI）。算法契约与 rust/native（fingerprint.rs）严格一致。
func machineFingerprint() (FingerprintResult, error) {
	fields := map[string]string{}

	if guid, err := readMachineGuid(); err == nil && guid != "" {
		fields["machine_guid"] = guid
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		fields["hostname"] = h
	}

	const biosKey = `HARDWARE\DESCRIPTION\System\BIOS`
	insert := func(k, v string, ok bool) {
		if ok && isValidSerial(v) {
			fields[k] = v
		}
	}
	v, ok1 := regGetString(biosKey, "BaseBoardSerialNumber")
	insert("board_serial", v, ok1)
	v, ok2 := regGetString(biosKey, "SystemSerialNumber")
	insert("bios_serial", v, ok2)
	v, ok3 := regGetString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "Identifier")
	insert("cpu_identifier", v, ok3)
	v, ok4 := readGpuNames()
	insert("gpu_names", v, ok4)
	if kb, okMem := physicallyInstalledKB(); okMem && kb > 0 {
		fields["ram_bytes"] = strconv.FormatUint(kb*1024, 10)
	}

	hash := combineHash(fields)
	return FingerprintResult{Hash: hash, Fields: fields}, nil
}

// regGetString 注册表直读 REG_SZ 值（RegGetValueW）。键不存在/类型不符返回 false。
func regGetString(subKey, value string) (string, bool) {
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		hklm,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(subKey))),
		0, uintptr(keyRead), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return "", false
	}
	defer procRegCloseKey.Call(uintptr(h))

	namePtr := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(value)))
	var size uint32
	r, _, _ = procRegQueryValueExW.Call(uintptr(h), namePtr, 0, 0, 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size == 0 {
		return "", false
	}
	buf := make([]uint16, size/2+1)
	r, _, _ = procRegQueryValueExW.Call(uintptr(h), namePtr,
		0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", false
	}
	text := strings.TrimSpace(syscall.UTF16ToString(buf))
	if text == "" {
		return "", false
	}
	return text, true
}

// readGpuNames 枚举显示适配器类子键 0000~0031 的 DriverDesc（过滤驱动未装占位）。
func readGpuNames() (string, bool) {
	const class = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`
	var names []string
	for i := 0; i < 32; i++ {
		if desc, ok := regGetString(fmt.Sprintf(class+`\%04d`, i), "DriverDesc"); ok {
			lower := strings.ToLower(desc)
			if !strings.Contains(lower, "microsoft basic display") {
				names = append(names, lower)
			}
		}
	}
	if len(names) == 0 {
		return "", false
	}
	return strings.Join(names, "|"), true
}

// physicallyInstalledKB 物理内存 KB（GetPhysicallyInstalledSystemMemory，SMBIOS 口径）。
func physicallyInstalledKB() (uint64, bool) {
	var kb uint64
	r, _, _ := procGetPhysicallyInstalledSystemMemory.Call(uintptr(unsafe.Pointer(&kb)))
	if r == 0 || kb == 0 {
		return 0, false
	}
	return kb, true
}

// isValidSerial 厂商占位值黑名单（与 rust is_valid_serial 同表）。
func isValidSerial(v string) bool {
	t := strings.ToLower(strings.TrimSpace(v))
	switch t {
	case "", "none", "default", "default string", "to be filled by o.e.m.",
		"system serial number", "serial number", "0", "000", "000000000":
		return false
	}
	return true
}

// macAddresses 本机 MAC 列表（小写排序）——虚拟机检测用（hasVMMACPrefix）。
func macAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, i := range ifaces {
		if i.HardwareAddr.String() != "" && strings.Count(i.HardwareAddr.String(), ":") >= 5 {
			out = append(out, strings.ToLower(i.HardwareAddr.String()))
		}
	}
	sort.Strings(out)
	return out
}

// combineHash 按 key 排序拼接 "k=v;" 段后 MD5 hex（32 位）——与 rust combine_hash 同构。
func combineHash(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(fields[k])
		sb.WriteByte(';')
	}
	sum := md5.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}
