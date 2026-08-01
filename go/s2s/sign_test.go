package s2s

import (
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	appID := "APP_test"
	secret := "fake-secret-for-test"
	body := []byte(`{"paths":["uploads/test.zip"]}`)
	ts := int64(1700000000)

	sig := Sign(appID, secret, ts, body)
	if sig == "" {
		t.Fatal("签名结果为空")
	}

	// 正确签名应通过
	if !Verify(appID, secret, ts, body, sig) {
		t.Fatal("正确签名应通过 Verify")
	}

	// 错误密钥应失败
	if Verify(appID, "wrong-secret", ts, body, sig) {
		t.Fatal("错误密钥不应通过 Verify")
	}

	// 篡改 body 应失败
	if Verify(appID, secret, ts, []byte(`{"paths":["evil"]}`), sig) {
		t.Fatal("篡改 body 后不应通过 Verify")
	}

	// 篡改 appID 应失败
	if Verify("APP_evil", secret, ts, body, sig) {
		t.Fatal("篡改 appID 后不应通过 Verify")
	}
}

func TestSignStable(t *testing.T) {
	// 相同输入签名必须稳定（服务端按同算法重算）
	sig1 := Sign("APP_x", "secret", 1700000000, []byte("hello"))
	sig2 := Sign("APP_x", "secret", 1700000000, []byte("hello"))
	if sig1 != sig2 {
		t.Fatalf("相同输入签名不稳定: %s vs %s", sig1, sig2)
	}
}

func TestSignHeadersContainsRequiredFields(t *testing.T) {
	h := SignHeaders("APP_x", "secret", []byte("{}"))
	for _, key := range []string{"X-App-ID", "X-S2S-Timestamp", "X-S2S-Signature"} {
		if _, ok := h[key]; !ok {
			t.Errorf("SignHeaders 缺少必需字段: %s", key)
		}
	}
	if h["X-App-ID"] != "APP_x" {
		t.Errorf("X-App-ID 错误: %s", h["X-App-ID"])
	}
}
