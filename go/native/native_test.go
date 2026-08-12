package native

import (
	"strings"
	"testing"
)

func TestSignRequestRoundTrip(t *testing.T) {
	n := New("app_test", "secret_123", "https://api.example.com")
	res, err := n.SignRequest("dev_001", `{"a":1}`)
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if len(res.Sign) != 64 {
		t.Errorf("sign 应为 64 hex，实际 %d", len(res.Sign))
	}
	if res.Timestamp == "" || len(res.Nonce) < 16 {
		t.Errorf("timestamp/nonce 缺失: %+v", res)
	}
	// 重放验证：同一签名数据应得到相同签名（供服务端复算）
	res2, err := signRequest("secret_123", "dev_001", `{"a":1}`)
	if err != nil || res2.Sign != res.Sign {
		t.Error("同参数签名应一致")
	}
}

func TestVerifyUpgrade(t *testing.T) {
	n := New("app_test", "secret_123", "")
	manifest := `{"has_update":true,"latest_version":"1.0.0"}`
	// 先伪造错误签名
	if ok, _ := n.VerifyUpgrade(manifest, "deadbeef"); ok {
		t.Error("错误签名应校验失败")
	}
	sig := hmacSign("secret_123", manifest)
	if ok, _ := n.VerifyUpgrade(manifest, sig); !ok {
		t.Error("正确签名应校验通过")
	}
}

func TestMachineFingerprintDeterministic(t *testing.T) {
	a, err := machineFingerprint()
	if err != nil {
		t.Fatalf("machineFingerprint: %v", err)
	}
	b, _ := machineFingerprint()
	if a.Hash == "" || a.Hash != b.Hash {
		t.Error("指纹应确定且非空")
	}
	if len(a.Fields) == 0 {
		t.Error("指纹字段不应为空")
	}
}

func TestDetectRisksNoPanic(t *testing.T) {
	res, err := detectRisks()
	if err != nil {
		t.Fatalf("detectRisks: %v", err)
	}
	for _, f := range res.Flags {
		switch f {
		case "debug", "emulator", "root", "hook", "multiopen":
		default:
			t.Errorf("未知风险 flag: %s", f)
		}
	}
}

func TestSelfIntegrity(t *testing.T) {
	res, err := executableHash()
	if err != nil {
		t.Fatalf("executableHash: %v", err)
	}
	if len(res) != 32 {
		t.Errorf("SHA256 长度应为 32，实际 %d", len(res))
	}
}

func TestAcquireMutexTwice(t *testing.T) {
	first, err := acquireMutex("test_app_mutex")
	if err != nil {
		t.Fatalf("acquireMutex: %v", err)
	}
	if !first {
		t.Skip("环境已存在实例锁")
	}
	second, _ := acquireMutex("test_app_mutex")
	if second {
		t.Error("同一进程第二次获取应返回 false")
	}
}

func TestSecureStoreRoundTrip(t *testing.T) {
	key := "test_token_1"
	_ = secureSet(key, "secret-value-xyz")
	got, err := secureGet(key)
	if err != nil {
		t.Fatalf("secureGet: %v", err)
	}
	if got != "secret-value-xyz" {
		t.Errorf("读取值不一致: %q", got)
	}
}

func TestSignDataFormat(t *testing.T) {
	// 与平台 DeviceSign 协议对齐：deviceID\n timestamp \n body
	sig, err := signRequest("k", "dev", "body")
	if err != nil {
		t.Fatal(err)
	}
	// 直接按协议构造签名数据复算
	signData := "dev\n" + sig.Timestamp + "\nbody"
	if !strings.EqualFold(hmacSign("k", signData), sig.Sign) {
		t.Error("签名协议与平台不一致")
	}
}
