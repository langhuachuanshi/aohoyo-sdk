package native

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// hmacSign 测试辅助：HMAC-SHA256 hex
func hmacSign(key, data string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
