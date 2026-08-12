//go:build !windows

package native

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"sort"
	"strings"
)

// machineFingerprint 非 Windows 指纹：machine-id + hostname + MAC 组合。
func machineFingerprint() (FingerprintResult, error) {
	fields := map[string]string{}

	if b, err := os.ReadFile("/etc/machine-id"); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" {
			fields["machine_id"] = v
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		fields["hostname"] = h
	}
	if macs := macAddresses(); len(macs) > 0 {
		fields["mac"] = strings.Join(macs, ",")
	}

	hash := combineHash(fields)
	return FingerprintResult{Hash: hash, Fields: fields}, nil
}

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
