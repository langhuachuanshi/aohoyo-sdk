//go:build windows

package native

import (
	"encoding/base64"
	"errors"
)

// secureGet 从 DPAPI 读取（base64 编码后存储）。
func secureGet(key string) (string, error) {
	blob, err := readSecureFile(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", errors.New("安全存储数据损坏")
	}
	out, err := unprotectData(raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// secureSet 用 DPAPI 加密后写入本地安全存储。
func secureSet(key, val string) error {
	enc, err := protectData([]byte(val))
	if err != nil {
		return err
	}
	return writeSecureFile(key, base64.StdEncoding.EncodeToString(enc))
}
