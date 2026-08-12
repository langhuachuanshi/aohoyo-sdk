package native

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// signRequest DeviceSign 签名 + 一次性 nonce。
// 与平台 AS `pkg/sign` 完全同构：signData = deviceID + "\n" + timestamp + "\n" + body。
func signRequest(appSecret, deviceID, body string) (SignResult, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signData := deviceID + "\n" + ts + "\n" + body

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(signData))
	signature := hex.EncodeToString(mac.Sum(nil))

	nonce, err := randomHex(16)
	if err != nil {
		return SignResult{}, err
	}
	return SignResult{Sign: signature, Timestamp: ts, Nonce: nonce}, nil
}

// verifyUpgrade 校验升级清单签名。服务端签名 = HMAC(app_secret, manifestJSON)。
func verifyUpgrade(appSecret, manifestJSON, signature string) (bool, error) {
	if signature == "" {
		return false, nil
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(manifestJSON))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature)), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
