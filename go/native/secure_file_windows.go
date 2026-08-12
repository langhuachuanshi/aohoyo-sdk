//go:build windows

package native

import (
	"os"
	"path/filepath"
)

// 安全存储文件位置（DPAPI 密文落盘）
func secureFilePath(key string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "aohoyo", "secure", sanitizeKey(key))
}

func readSecureFile(key string) (string, error) {
	b, err := os.ReadFile(secureFilePath(key))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeSecureFile(key, val string) error {
	path := secureFilePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(val), 0o600)
}

func sanitizeKey(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "_"
	}
	return string(out)
}
