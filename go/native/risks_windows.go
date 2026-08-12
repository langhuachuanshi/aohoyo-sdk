//go:build windows

package native

import (
	"strings"
)

// detectRisks Windows 风险检测（威慑层，可被绕过）。
func detectRisks() (RisksResult, error) {
	var flags []string

	if isDebuggerPresent() {
		flags = append(flags, "debug")
	}
	if isVMAgentPresent() || hasVMMACPrefix() {
		flags = append(flags, "emulator")
	}
	if isElevated() {
		flags = append(flags, "root")
	}
	if hasHookModules() {
		flags = append(flags, "hook")
	}
	if hasSandboxModules() {
		flags = append(flags, "multiopen")
	}
	return RisksResult{Flags: flags}, nil
}

// isVMAgentPresent 检查常见 VM Guest Additions 注册表键
func isVMAgentPresent() bool {
	return registryKeyExists(hklm, `SOFTWARE\VMware, Inc.`) ||
		registryKeyExists(hklm, `SOFTWARE\Oracle\VirtualBox Guest Additions`) ||
		registryKeyExists(hklm, `SOFTWARE\Microsoft\Virtual Machine\Guest\Parameters`)
}

// hasVMMACPrefix 检查 MAC 地址厂商前缀
func hasVMMACPrefix() bool {
	prefixes := []string{"00:0c:29", "00:50:56", "00:05:69", "08:00:27", "52:54:00", "00:15:5d"}
	for _, mac := range macAddresses() {
		for _, p := range prefixes {
			if strings.HasPrefix(mac, p) {
				return true
			}
		}
	}
	return false
}

// hookModuleBlacklist hook/调试注入常见模块
var hookModuleBlacklist = []string{
	"frida-gadget", "frida-agent", "frida-core", "scylla", "megadump",
	"x64dbg", "ollydbg", "cheatengine", "speedhack", "inject",
}

func hasHookModules() bool {
	for _, m := range loadedModules() {
		for _, b := range hookModuleBlacklist {
			if strings.Contains(m, b) {
				return true
			}
		}
	}
	return false
}

// hasSandboxModules 检测沙箱/多开环境（Sandboxie 等）
func hasSandboxModules() bool {
	for _, m := range loadedModules() {
		if strings.Contains(m, "sbie") {
			return true
		}
	}
	return false
}
