//go:build windows

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// machineFingerprint Windows 指纹 v2：MachineGuid + hostname + 主板序列号 + BIOS 序列号。
// 算法契约与 rust/native（fingerprint.rs）严格一致——同机两端必须算出同 hash。
// 序列号经 PowerShell Get-CimInstance 读取（零三方依赖），厂商占位值（None/Default string 等）跳过。
func machineFingerprint() (FingerprintResult, error) {
	fields := map[string]string{}

	if guid, err := readMachineGuid(); err == nil && guid != "" {
		fields["machine_guid"] = guid
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		fields["hostname"] = h
	}
	for k, v := range readHwSignals() {
		fields[k] = v
	}

	hash := combineHash(fields)
	return FingerprintResult{Hash: hash, Fields: fields}, nil
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

// readHwSignals PowerShell 一次调用取全部硬件信号（与 rust read_hw_signals 同一脚本/字段）。
// 返回：board_serial / bios_serial / cpu_id / gpu_names / ram_bytes。
func readHwSignals() map[string]string {
	out := map[string]string{}
	script := "(Get-CimInstance Win32_BaseBoard).SerialNumber; " +
		"(Get-CimInstance Win32_BIOS).SerialNumber; " +
		"(Get-CimInstance Win32_Processor | Select-Object -First 1).ProcessorId; " +
		"(Get-CimInstance Win32_VideoController | Sort-Object Name | ForEach-Object Name) -join '|'; " +
		"(Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum"
	raw, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return out
	}
	var rows []string
	for _, l := range strings.Split(string(raw), "\r\n") {
		l = strings.TrimSpace(l)
		rows = append(rows, l)
	}
	pick := func(i int) string {
		if i < len(rows) {
			return rows[i]
		}
		return ""
	}
	insert := func(k, v string) {
		if isValidSerial(v) {
			out[k] = v
		}
	}
	insert("board_serial", pick(0))
	insert("bios_serial", pick(1))
	insert("cpu_id", pick(2))
	var gpus []string
	for _, g := range strings.Split(pick(3), "|") {
		g = strings.TrimSpace(g)
		if g != "" && !strings.Contains(strings.ToLower(g), "microsoft basic display") {
			gpus = append(gpus, strings.ToLower(g))
		}
	}
	if len(gpus) > 0 {
		out["gpu_names"] = strings.Join(gpus, "|")
	}
	if sum := strings.TrimSpace(pick(4)); sum != "" && sum != "0" {
		out["ram_bytes"] = sum
	}
	return out
}

// combineHash 按 key 排序拼接 "k=v;" 段后 SHA256 hex——与 rust combine_hash 同构。
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
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
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
