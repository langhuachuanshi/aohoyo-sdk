package native

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CheckRequest 升级检测请求。Platform 建议必传——服务端按平台取「该平台有包的最新版本」
// （平台独立节奏），不传回退全局最新 + 第一个包（仅兼容旧版本）。
type CheckRequest struct {
	CurrentVersionCode int    `json:"current_version_code"`
	Platform           string `json:"platform,omitempty"` // windows/macos/linux/android/ios
	ChannelCode        string `json:"channel_code,omitempty"`
	DeviceID           string `json:"device_id,omitempty"` // 灰度确定性分配依赖此值
}

// CheckResult 升级检测结果，字段与服务端 /as/v1/upgrade/check 响应 data 一一对应。
type CheckResult struct {
	HasUpdate         bool   `json:"has_update"`
	ForceUpdate       bool   `json:"force_update"` // true 时宿主必须阻断用户操作
	LatestVersion     string `json:"latest_version"`
	LatestVersionCode int    `json:"latest_version_code"`
	Platform          string `json:"platform"`
	DownloadURL       string `json:"download_url"` // 空 = 该平台暂无包，提示用户而非静默
	FileSize          int64  `json:"file_size"`
	MD5               string `json:"md5"`
	SHA256            string `json:"sha256"`
	UpdateLog         string `json:"update_log"`
	Signature         string `json:"signature,omitempty"` // 应用开启 upgrade_signature 时返回

	// RawManifest 服务端响应 data 的原始 JSON（已删 signature 尾段）。
	// VerifyCheckResult 用它复算 HMAC，与服务端签名字节严格一致（不走重新序列化，无转义/键序差异）。
	RawManifest string `json:"-"`
}

// CheckUpgrade 调用平台升级检测接口 POST /as/v1/upgrade/check（公开接口）。
func (n *Native) CheckUpgrade(req CheckRequest) (*CheckResult, error) {
	body, _ := json.Marshal(map[string]any{
		"app_id":               n.AppID,
		"current_version_code": req.CurrentVersionCode,
		"platform":             req.Platform,
		"channel_code":         req.ChannelCode,
		"device_id":            req.DeviceID,
	})

	httpc := n.httpClient()
	resp, err := httpc.Post(strings.TrimRight(n.BaseURL, "/")+"/as/v1/upgrade/check",
		"application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("升级检测请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("升级检测 HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if envelope.Code != 200 {
		return nil, fmt.Errorf("升级检测失败: %s", envelope.Message)
	}

	var r CheckResult
	if err := json.Unmarshal(envelope.Data, &r); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	r.RawManifest = stripSignature(string(envelope.Data))
	return &r, nil
}

// VerifyCheckResult 校验清单签名。应用未开启签名（Signature 为空）时直接通过。
func (n *Native) VerifyCheckResult(r *CheckResult) (bool, error) {
	if r.Signature == "" {
		return true, nil
	}
	if n.AppSecret == "" {
		return false, fmt.Errorf("app_secret 未配置")
	}
	return verifyUpgrade(n.AppSecret, r.RawManifest, r.Signature)
}

// stripSignature 字节级删除 JSON 尾部的 ,"signature":"..." 段。
// 服务端 CheckResp 的 signature 是最后一个字段（omitempty），删除后即为被签名的 manifest 原文。
func stripSignature(s string) string {
	const marker = `,"signature":"`
	if i := strings.LastIndex(s, marker); i >= 0 {
		return s[:i] + "}"
	}
	return s
}
