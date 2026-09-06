package native

// AD 客户端接口（主仓库 docs/specs/ad.md）：
//
//	GET  /as/v1/ads             JWT 或 DeviceSign（免登录桌面端走 DeviceSign，GET 空 body 参与签名）
//	POST /as/v1/ads/impression  公开（限流 100/min）
//	POST /as/v1/ads/click       公开（限流 100/min）
//
// 响应结构均与 admin-server model.AdClientItem / AdClientResp 对应（服务端已脱敏，不含广告主信息）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	endpointAds           = "/as/v1/ads"
	endpointAdsImpression = "/as/v1/ads/impression"
	endpointAdsClick      = "/as/v1/ads/click"
)

// AdItem 客户端广告项。
type AdItem struct {
	ID         int64                  `json:"id"`
	PositionID int64                  `json:"position_id"`
	Title      string                 `json:"title"`     // 文字广告即内容，其他类型作 alt
	Content    string                 `json:"content"`   // 文字广告内容
	ImageURL   string                 `json:"image_url"` // 图片/开屏/横幅
	LinkURL    string                 `json:"link_url"`
	AdType     int16                  `json:"ad_type"` // 1文字 2图片 3弹窗 4开屏 5横幅
	Width      int                    `json:"width"`
	Height     int                    `json:"height"`
	StartTime  *time.Time             `json:"start_time"`
	EndTime    *time.Time             `json:"end_time"`
	Config     map[string]interface{} `json:"config"` // 广告级扩展配置
}

// ClientAds 拉取广告响应：key = 广告位 code（如 splash、home-banner）。
type ClientAds struct {
	Positions map[string][]AdItem `json:"positions"`
}

// AdReportResult 曝光/点击上报结果（服务端 OKMsg，data 为空）。
type AdReportResult struct{}

// getSigned 发送 DeviceSign 签名的 GET（签名串 body 段为空串），解包统一响应。
func (n *Native) getSigned(ctx context.Context, path string, query url.Values, deviceID string, out any) error {
	if n.AppSecret == "" {
		return fmt.Errorf("app_secret 未配置")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sign, err := n.SignRequest(deviceID, "")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.BaseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
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
	return decodeEnvelope(resp, path, out)
}

// postPlain 发送无鉴权 JSON POST（公开接口），解包统一响应。
func (n *Native) postPlain(ctx context.Context, path string, payload any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("请求体序列化失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	return decodeEnvelope(resp, path, out)
}

// decodeEnvelope 解包 {code, message, data} 统一响应；out 为 nil 时忽略 data。
func decodeEnvelope(resp *http.Response, path string, out any) error {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("请求失败 (HTTP %d): %s: %s", resp.StatusCode, path, string(body))
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

// GetAds 拉取当前有效广告（DeviceSign 鉴权；登录态宿主也可自行带 JWT 调 HTTP）。
// deviceID 建议传机器指纹 hash（灰度/统计口径稳定）；positionCode 非空时只返回该广告位。
func (n *Native) GetAds(ctx context.Context, deviceID, positionCode string) (*ClientAds, error) {
	q := url.Values{}
	q.Set("app_id", n.AppID)
	if positionCode != "" {
		q.Set("position", positionCode)
	}
	var out ClientAds
	if err := n.getSigned(ctx, endpointAds, q, deviceID, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecordImpression 上报广告展示（公开接口）。deviceID 建议传机器指纹 hash，缺省记空。
func (n *Native) RecordImpression(ctx context.Context, adID int64, deviceID string) error {
	return n.postPlain(ctx, endpointAdsImpression, map[string]any{"ad_id": adID, "device_id": deviceID}, nil)
}

// RecordClick 上报广告点击（公开接口）。
func (n *Native) RecordClick(ctx context.Context, adID int64, deviceID string) error {
	return n.postPlain(ctx, endpointAdsClick, map[string]any{"ad_id": adID, "device_id": deviceID}, nil)
}
