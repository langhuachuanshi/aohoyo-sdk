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
	board, bios := readHwSerials()
	if board != "" {
		fields["board_serial"] = board
	}
	if bios != "" {
		fields["bios_serial"] = bios
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

// readHwSerials PowerShell 一次调用取主板/BIOS 序列号。
func readHwSerials() (board, bios string) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"(Get-CimInstance Win32_BaseBoard).SerialNumber; (Get-CimInstance Win32_BIOS).SerialNumber").Output()
	if err != nil {
		return "", ""
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\r\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 0 && isValidSerial(lines[0]) {
		board = lines[0]
	}
	if len(lines) > 1 && isValidSerial(lines[1]) {
		bios = lines[1]
	}
	return board, bios
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
