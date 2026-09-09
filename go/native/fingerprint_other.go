//go:build !windows

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

// machineFingerprint 非 Windows 指纹：machine-id + product_uuid（DMI），
// 两者皆空（macOS 等）退回 hostname——算法契约与 rust/native（fingerprint.rs）严格一致。
func machineFingerprint() (FingerprintResult, error) {
	fields := map[string]string{}

	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			fields["machine_id"] = v
		}
	}
	if b, err := os.ReadFile("/sys/class/dmi/id/product_uuid"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			fields["product_uuid"] = v
		}
	}
	if len(fields) == 0 {
		if h, err := os.Hostname(); err == nil && h != "" {
			fields["hostname"] = h
		}
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
