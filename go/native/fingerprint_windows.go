//go:build windows

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

// machineFingerprint Windows 指纹：MachineGuid + hostname 组合哈希。
// 算法契约与 rust/native（fingerprint.rs）严格一致——同机两端必须算出同 hash
// （2026-09-10 对齐：此前 go 用 MachineGuid+MAC，网卡/虚拟网卡变化即漂移，且与 rust 不一致）。
func machineFingerprint() (FingerprintResult, error) {
	fields := map[string]string{}

	if guid, err := readMachineGuid(); err == nil && guid != "" {
		fields["machine_guid"] = guid
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		fields["hostname"] = h
	}

	hash := combineHash(fields)
	return FingerprintResult{Hash: hash, Fields: fields}, nil
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
