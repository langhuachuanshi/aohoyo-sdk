//go:build !windows

package native

import (
	"os"
	"path/filepath"
)

// secureGet 非 Windows：从用户配置目录读取（0600 权限文件）。
// macOS 上如需 Keychain 级保护，可在宿主应用侧接入系统钥匙串。
func secureGet(key string) (string, error) {
	b, err := os.ReadFile(secureFilePath(key))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// secureSet 写入 0600 权限文件。
func secureSet(key, val string) error {
	path := secureFilePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(val), 0o600)
}

func secureFilePath(key string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "aohoyo", "secure", sanitizeKey(key))
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
