package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// detectRisksFunc 风险检测实现（测试可替换以固定 flags）。
var detectRisksFunc = detectRisks

const (
	endpointDevicesReport  = "/as/v1/devices/report"
	endpointSecurityConfig = "/as/v1/app/security/config"
	// reportTimeout 上报/策略拉取的单次请求超时
	reportTimeout = 10 * time.Second
)

// reportableRiskFlags 服务端 risk.Analyze 接受的 risk_flags 枚举。
// DetectRisks 的 debug 不在枚举内（服务端不落库），上报前丢弃。
var reportableRiskFlags = map[string]bool{
	"emulator": true,
	"multiopen": true,
	"root":     true,
	"hook":     true,
}

// mapRiskFlagsToReport 把 DetectRisks 的原始 flags 映射到服务端接受枚举（未知值丢弃）。
func mapRiskFlagsToReport(flags []string) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		if reportableRiskFlags[f] {
			out = append(out, f)
		}
	}
	return out
}

// DeviceReportInfo devices/report 请求体的设备信息部分（app_id/device_id 由方法补全）。
type DeviceReportInfo struct {
	UserID      int64  `json:"user_id"`
	DeviceBrand string `json:"device_brand"`
	DeviceModel string `json:"device_model"`
	OSVersion   string `json:"os_version"`
	AppVersion  string `json:"app_version"`
	ChannelCode string `json:"channel_code"`
}

// RiskBlockedError 安全策略 risk_policy=block 且检测到风险时返回（设备上报已完成）。
// 宿主应用 errors.As 捕获后展示阻断 UI 并自行决定是否中断启动流程。
type RiskBlockedError struct {
	Flags []string
}

func (e *RiskBlockedError) Error() string {
	return "检测到运行环境风险: " + strings.Join(e.Flags, ", ")
}

// SecurityConfig 桌面端安全策略（POST /as/v1/app/security/config 响应）。
// 字段与服务端 SecurityAPI.DesktopConfig 下发结构一一对应。
type SecurityConfig struct {
	AppID            string `json:"app_id"`
	Status           int    `json:"status"`
	AntiDebug        bool   `json:"anti_debug"`
	AntiMultiOpen    bool   `json:"anti_multi_open"`
	EmulatorDetect   bool   `json:"emulator_detect"`
	HookDetect       bool   `json:"hook_detect"`
	RootDetect       bool   `json:"root_detect"`
	ReplayProtection bool   `json:"replay_protection"`
	IntegrityCheck   bool   `json:"integrity_check"`
	MaxDevices       int    `json:"max_devices"`
	CertPin          string `json:"cert_pin"`
	LicenseMode      string `json:"license_mode"`
	OfflineGraceDays int    `json:"offline_grace_days"`
	RiskPolicy       string `json:"risk_policy"` // report / observe / block
	UpgradeSignature string `json:"upgrade_signature"`
	ConfigJSON       string `json:"config_json"`
}

// PruneRiskFlags 按策略开关裁剪检测项（如 emulator_detect=false 则丢弃 emulator）。
// nil 接收者直接原样返回（策略拉取失败时不裁剪）。
func (c *SecurityConfig) PruneRiskFlags(flags []string) []string {
	if c == nil {
		return flags
	}
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		var enabled bool
		switch f {
		case "emulator":
			enabled = c.EmulatorDetect
		case "root":
			enabled = c.RootDetect
		case "hook":
			enabled = c.HookDetect
		case "multiopen":
			enabled = c.AntiMultiOpen
		default:
			enabled = true
		}
		if enabled {
			out = append(out, f)
		}
	}
	return out
}

// httpClient 惰性初始化的共享 HTTP 客户端（Timeout 兜底，方法内部 ctx 可再收紧）。
func (n *Native) httpClient() *http.Client {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.httpc == nil {
		n.httpc = &http.Client{Timeout: reportTimeout}
	}
	return n.httpc
}

// postSigned 发送 DeviceSign 签名的 JSON POST，并解包 {code, data, message} 响应。
// payload 为 nil 时发空请求体（security/config 只看 X-App-ID 头）；out 为 nil 时忽略响应 data。
func (n *Native) postSigned(ctx context.Context, path, deviceID string, payload any, out any) error {
	if n.AppSecret == "" {
		return fmt.Errorf("app_secret 未配置")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return fmt.Errorf("请求体序列化失败: %w", err)
		}
	}

	sign, err := n.SignRequest(deviceID, string(body))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-App-ID", n.AppID)
	req.Header.Set("X-Device-Sign", sign.Sign)
	req.Header.Set("X-Device-ID", deviceID)
	req.Header.Set("X-Timestamp", sign.Timestamp)
	req.Header.Set("X-Nonce", sign.Nonce)

	resp, err := n.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("请求失败 (HTTP %d): %s", resp.StatusCode, path)
	}

	if out == nil {
		return nil
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != http.StatusOK {
		return fmt.Errorf("服务端返回错误 (%d): %s", envelope.Code, envelope.Message)
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("响应 data 解析失败: %w", err)
	}
	return nil
}

// GetSecurityConfig 拉取桌面端安全策略（DeviceSign 鉴权，头 X-App-ID）。
// 供宿主应用在启动时获取 risk_policy 与各检测项开关；失败返回 error，调用方可降级为默认策略（report）。
func (n *Native) GetSecurityConfig(ctx context.Context, deviceID string) (*SecurityConfig, error) {
	var cfg SecurityConfig
	if err := n.postSigned(ctx, endpointSecurityConfig, deviceID, nil, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ReportDeviceWithRisks 检测运行环境风险并上报设备（POST /as/v1/devices/report，DeviceSign 签名）。
//
// 流程：DetectRisks → 枚举映射（debug 等服务端不接受的值丢弃）→ 尽力拉取安全策略
// 裁剪检测项（策略拉取失败不阻断上报，按未裁剪 flags 上报）→ 签名上报。
// 网络/服务端失败返回 error，调用方仅记日志即可，不应据此阻断业务流程。
// 若策略 risk_policy=block 且裁剪后仍有风险，上报完成后返回 *RiskBlockedError，
// 宿主应用可 errors.As 识别并展示阻断 UI（SDK 不自行中断）。
func (n *Native) ReportDeviceWithRisks(ctx context.Context, deviceID string, info DeviceReportInfo) error {
	res, err := detectRisksFunc()
	if err != nil {
		return fmt.Errorf("风险检测失败: %w", err)
	}
	flags := mapRiskFlagsToReport(res.Flags)

	cfg, cfgErr := n.GetSecurityConfig(ctx, deviceID)
	if cfgErr == nil {
		flags = cfg.PruneRiskFlags(flags)
	}

	if len(flags) == 0 {
		flags = []string{} // 保证 risk_flags 序列化为 [] 而非 null
	}
	body := map[string]any{
		"app_id":       n.AppID,
		"device_id":    deviceID,
		"user_id":      info.UserID,
		"device_brand": info.DeviceBrand,
		"device_model": info.DeviceModel,
		"os_version":   info.OSVersion,
		"app_version":  info.AppVersion,
		"channel_code": info.ChannelCode,
		"ip":           "", // 服务端从请求提取 ClientIP
		"risk_flags":   flags,
	}
	if err := n.postSigned(ctx, endpointDevicesReport, deviceID, body, nil); err != nil {
		return err
	}

	if cfg != nil && cfg.RiskPolicy == "block" && len(flags) > 0 {
		return &RiskBlockedError{Flags: flags}
	}
	return nil
}
