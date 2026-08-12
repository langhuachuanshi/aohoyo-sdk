//go:build !windows

package native

import (
	"os"
	"strings"
)

// detectRisks 非 Windows 风险检测（Linux/macOS 基础版）。
func detectRisks() (RisksResult, error) {
	var flags []string

	if linuxTracerPresent() {
		flags = append(flags, "debug")
	}
	if isVMByDMI() {
		flags = append(flags, "emulator")
	}
	if os.Geteuid() == 0 {
		flags = append(flags, "root")
	}
	if ldPreload := os.Getenv("LD_PRELOAD"); ldPreload != "" {
		flags = append(flags, "hook")
	}
	return RisksResult{Flags: flags}, nil
}

// linuxTracerPresent 读取 /proc/self/status 的 TracerPid（Linux 调试附加检测）
func linuxTracerPresent() bool {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "TracerPid:") {
			pid := strings.TrimSpace(strings.TrimPrefix(line, "TracerPid:"))
			return pid != "0"
		}
	}
	return false
}

// isVMByDMI 读取 DMI 信息检测虚拟机
func isVMByDMI() bool {
	markers := []string{"vmware", "virtualbox", "qemu", "kvm", "xen", "bochs"}
	for _, f := range []string{
		"/sys/class/dmi/id/product_name",
		"/sys/class/dmi/id/product_version",
		"/sys/class/dmi/id/sys_vendor",
	} {
		if b, err := os.ReadFile(f); err == nil {
			v := strings.ToLower(string(b))
			for _, m := range markers {
				if strings.Contains(v, m) {
					return true
				}
			}
		}
	}
	return false
}
