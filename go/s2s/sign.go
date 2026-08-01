// Package s2s 提供 aohoyo admin-server 应用级 S2S（Server-to-Server）签名与调用客户端。
//
// 协议（与 admin-server 的 DeviceSign 同构，把 deviceID 换成 appID）：
//
//	signData  = appID + "\n" + timestamp + "\n" + body
//	signature = HMAC-SHA256(appSecret, signData)  (hex 编码)
//
// 请求头：
//
//	X-App-ID:        APP_xxx
//	X-S2S-Timestamp: <秒级时间戳>
//	X-S2S-Signature: <hex>
//
// 服务端校验时间戳必须在 5 分钟内（防重放），并按 appID 查 apps.app_secret 重算签名对比（防篡改）。
//
// 最小示例：
//
//	c := s2s.New("APP_wilas_xxx", "your-app-secret", "https://admin.example.com")
//	res, err := c.Upload(context.Background(), "uploads/", "test.zip", data, int64(len(data)))
//	fmt.Println(res.URL)
package s2s

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// Sign 用 appSecret 对 (appID, timestamp, body) 计算 HMAC-SHA256 签名，返回 hex。
// 与 admin-server pkg/sign.BuildS2SSignData + HMACSha256 保持完全一致的拼接规则。
func Sign(appID, appSecret string, timestamp int64, body []byte) string {
	signData := appID + "\n" + strconv.FormatInt(timestamp, 10) + "\n" + string(body)
	h := hmac.New(sha256.New, []byte(appSecret))
	h.Write([]byte(signData))
	return hex.EncodeToString(h.Sum(nil))
}

// SignHeaders 为给定 body 计算签名并返回完整的 S2S 请求头。
// appID/appSecret 来自 admin-server 的 apps 表（应用密钥）。
func SignHeaders(appID, appSecret string, body []byte) map[string]string {
	ts := time.Now().Unix()
	sig := Sign(appID, appSecret, ts, body)
	return map[string]string{
		"X-App-ID":        appID,
		"X-S2S-Timestamp": strconv.FormatInt(ts, 10),
		"X-S2S-Signature": sig,
	}
}

// Verify 服务端侧可选工具：校验签名是否匹配（用于自测或回环验证）。
func Verify(appID, appSecret string, timestamp int64, body []byte, signature string) bool {
	expected := Sign(appID, appSecret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// TimestampNow 返回当前秒级时间戳字符串（用于自定义请求时填充 header）。
func TimestampNow() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// CheckTolerance 检查时间戳是否在容忍窗口内（单位：秒）。admin-server 默认 5 分钟 = 300。
func CheckTolerance(timestampStr string, toleranceSeconds int64) error {
	ts, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("时间戳格式错误")
	}
	diff := time.Since(time.Unix(ts, 0))
	if diff > time.Duration(toleranceSeconds)*time.Second ||
		diff < -time.Duration(toleranceSeconds)*time.Second {
		return fmt.Errorf("签名已过期")
	}
	return nil
}
