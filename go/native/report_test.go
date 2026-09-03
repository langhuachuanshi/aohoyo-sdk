package native

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// stubDetectRisks 固定风险检测结果，测试结束后恢复
func stubDetectRisks(t *testing.T, flags []string) {
	t.Helper()
	old := detectRisksFunc
	detectRisksFunc = func() (RisksResult, error) {
		return RisksResult{Flags: flags}, nil
	}
	t.Cleanup(func() { detectRisksFunc = old })
}

// securityConfigResponse 构造 /app/security/config 的服务端响应
func securityConfigResponse(riskPolicy string, emulatorDetect bool) string {
	body, _ := json.Marshal(map[string]any{
		"code":    200,
		"message": "success",
		"data": map[string]any{
			"app_id":             "app_test",
			"status":             1,
			"anti_debug":         true,
			"anti_multi_open":    true,
			"emulator_detect":    emulatorDetect,
			"hook_detect":        true,
			"root_detect":        true,
			"replay_protection":  false,
			"integrity_check":    false,
			"max_devices":        3,
			"cert_pin":           "",
			"license_mode":       "free",
			"offline_grace_days": 0,
			"risk_policy":        riskPolicy,
			"upgrade_signature":  "",
			"config_json":        "",
		},
	})
	return string(body)
}

func TestMapRiskFlagsToReport(t *testing.T) {
	got := mapRiskFlagsToReport([]string{"debug", "emulator", "multiopen", "root", "hook", "sign", "unknown"})
	want := []string{"debug", "emulator", "multiopen", "root", "hook"}
	if len(got) != len(want) {
		t.Fatalf("枚举映射结果 %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("枚举映射结果 %v，期望 %v", got, want)
		}
	}
	if len(mapRiskFlagsToReport(nil)) != 0 {
		t.Error("空 flags 应返回空切片")
	}
}

func TestPruneRiskFlags(t *testing.T) {
	flags := []string{"debug", "emulator", "root", "hook", "multiopen"}

	var nilCfg *SecurityConfig
	if got := nilCfg.PruneRiskFlags(flags); len(got) != len(flags) {
		t.Error("nil 策略不应裁剪")
	}

	cfg := &SecurityConfig{AntiDebug: false, EmulatorDetect: false, AntiMultiOpen: true, RootDetect: true, HookDetect: true}
	got := cfg.PruneRiskFlags(flags)
	if len(got) != 3 || contains(got, "emulator") || contains(got, "debug") {
		t.Errorf("anti_debug/emulator_detect=false 应丢弃 debug/emulator，实际 %v", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestGetSecurityConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointSecurityConfig {
			t.Errorf("请求路径 %s，期望 %s", r.URL.Path, endpointSecurityConfig)
		}
		if r.Header.Get("X-App-ID") != "app_test" {
			t.Errorf("缺少 X-App-ID 头: %v", r.Header)
		}
		io.WriteString(w, securityConfigResponse("observe", true))
	}))
	defer srv.Close()

	n := New("app_test", "secret_123", srv.URL)
	cfg, err := n.GetSecurityConfig(context.Background(), "dev_001")
	if err != nil {
		t.Fatalf("GetSecurityConfig: %v", err)
	}
	if cfg.RiskPolicy != "observe" || !cfg.EmulatorDetect || cfg.MaxDevices != 3 {
		t.Errorf("策略解析错误: %+v", cfg)
	}
}

func TestGetSecurityConfigSecretMissing(t *testing.T) {
	n := New("app_test", "", "http://localhost")
	if _, err := n.GetSecurityConfig(context.Background(), "dev_001"); err == nil {
		t.Error("app_secret 未配置应报错")
	}
}

func TestReportDeviceWithRisksSignature(t *testing.T) {
	stubDetectRisks(t, []string{"emulator", "debug"})

	var mu sync.Mutex
	var gotBody string
	var gotSign, gotTS, gotDeviceID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		gotBody = string(b)
		gotSign = r.Header.Get("X-Device-Sign")
		gotTS = r.Header.Get("X-Timestamp")
		gotDeviceID = r.Header.Get("X-Device-ID")
		// security/config 拉取失败（404）→ 走不裁剪降级路径
		if r.URL.Path == endpointSecurityConfig {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":404,"message":"安全配置不存在"}`)
			return
		}
		io.WriteString(w, `{"code":200,"message":"success","data":{"id":1}}`)
	}))
	defer srv.Close()

	n := New("app_test", "secret_123", srv.URL)
	err := n.ReportDeviceWithRisks(context.Background(), "dev_001", DeviceReportInfo{
		DeviceBrand: "ACME", DeviceModel: "PC-1", OSVersion: "Windows 11", AppVersion: "10203", ChannelCode: "official",
	})
	if err != nil {
		t.Fatalf("ReportDeviceWithRisks: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// 签名复算（与 AS pkg/sign 同构）：HMAC(app_secret, deviceID + "\n" + ts + "\n" + body)
	if expected := hmacSign("secret_123", gotDeviceID+"\n"+gotTS+"\n"+gotBody); expected != gotSign {
		t.Errorf("DeviceSign 签名不匹配:\n期望 %s\n实际 %s", expected, gotSign)
	}

	var req struct {
		AppID     string   `json:"app_id"`
		DeviceID  string   `json:"device_id"`
		RiskFlags []string `json:"risk_flags"`
	}
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("请求体解析: %v", err)
	}
	if req.AppID != "app_test" || req.DeviceID != "dev_001" {
		t.Errorf("app_id/device_id 错误: %+v", req)
	}
	// debug/emulator 均在服务端枚举内应保留（保持检测输出顺序）；策略拉取失败不裁剪
	if len(req.RiskFlags) != 2 || req.RiskFlags[0] != "emulator" || req.RiskFlags[1] != "debug" {
		t.Errorf("risk_flags 期望 [emulator debug]，实际 %v", req.RiskFlags)
	}
}

func TestReportDeviceWithRisksBlockPolicy(t *testing.T) {
	stubDetectRisks(t, []string{"debug", "emulator", "hook"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == endpointSecurityConfig {
			io.WriteString(w, securityConfigResponse("block", true))
			return
		}
		io.WriteString(w, `{"code":200,"message":"success","data":{"id":1}}`)
	}))
	defer srv.Close()

	n := New("app_test", "secret_123", srv.URL)
	err := n.ReportDeviceWithRisks(context.Background(), "dev_001", DeviceReportInfo{})
	var blocked *RiskBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("block 策略 + 有风险应返回 RiskBlockedError，实际: %v", err)
	}
	if len(blocked.Flags) != 3 {
		t.Errorf("阻断 flags 期望 [debug emulator hook]，实际 %v", blocked.Flags)
	}
	if !strings.Contains(blocked.Error(), "debug") {
		t.Errorf("错误信息应包含风险项 debug: %s", blocked.Error())
	}
}

func TestReportDeviceWithRisksBlockPrunedToEmpty(t *testing.T) {
	// 检测到 emulator 但策略 emulator_detect=false → 裁剪后无风险，不阻断
	stubDetectRisks(t, []string{"emulator"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == endpointSecurityConfig {
			io.WriteString(w, securityConfigResponse("block", false))
			return
		}
		io.WriteString(w, `{"code":200,"message":"success","data":{"id":1}}`)
	}))
	defer srv.Close()

	n := New("app_test", "secret_123", srv.URL)
	if err := n.ReportDeviceWithRisks(context.Background(), "dev_001", DeviceReportInfo{}); err != nil {
		t.Errorf("裁剪后无风险不应阻断，实际: %v", err)
	}
}

func TestReportDeviceWithRisksServerFail(t *testing.T) {
	stubDetectRisks(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := New("app_test", "secret_123", srv.URL)
	if err := n.ReportDeviceWithRisks(context.Background(), "dev_001", DeviceReportInfo{}); err == nil {
		t.Error("服务端 500 应返回 error（调用方仅记日志）")
	}
}
