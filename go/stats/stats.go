// Package stats 提供 aohoyo admin-server 统计事件上报客户端。
//
// 对应 AS 的公开接口 POST /api/v1/stats/events（无需鉴权，仅限流 60/min）。
// 自 v0.8.0 起 BaseURL 指向 /api 路由组根（同 s2s 包），本包只拼 /stats/events，
// 不再含 /api 前缀，避免经反代时出现 /api/api/stats/... 双前缀 404。
// 与 sdk/client-js 的 stats 模块对接同一个后端接口，字段定义完全一致。
//
// 事件类型（event_type）和平台（platform）受服务端 oneof 校验：
//
//	event_type: page_view | session_start | session_end | error | custom
//	platform:   ios | android | web | mini_program | windows
//
// 最小示例：
//
//	c := stats.New("APP_xxx", "https://admin.example.com")
//	err := c.ReportEvent(context.Background(), stats.Event{
//	    EventType: stats.EventCustom, SessionID: "s1",
//	    Platform: stats.PlatformWindows, Path: "/download", Title: "文件下载",
//	})
package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// 事件类型常量（与服务端 model.StatsEventReport 的 oneof 一致）
const (
	EventPageView     = "page_view"
	EventSessionStart = "session_start"
	EventSessionEnd   = "session_end"
	EventError        = "error"
	EventCustom       = "custom"
)

// 平台常量
const (
	PlatformIOS          = "ios"
	PlatformAndroid      = "android"
	PlatformWeb          = "web"
	PlatformMiniProgram  = "mini_program"
	PlatformWindows      = "windows"
)

const (
	// defaultTimeout 短请求的 context 超时（v0.9.0 起 HTTPClient.Timeout=0，由方法内部 context 管）。
	defaultTimeout = 10 * time.Second
	maxBatchSize   = 50 // 与服务端 StatsEventBatch 的 max=50 一致
	endpointEvents = "/v1/stats/events"
)

// Event 单条统计事件，字段与服务端 model.StatsEventReport 完全对齐。
//
// 必填：AppID、EventType、SessionID、Platform
// 其他字段按事件类型按需填（如 error 事件填 ErrorMsg/ErrorStack）。
type Event struct {
	AppID      string         `json:"app_id"`                // 必填（为空时用 Client.AppID 补全）
	EventType  string         `json:"event_type"`            // 必填（用上方常量）
	UserID     string         `json:"user_id,omitempty"`
	SessionID  string         `json:"session_id"`            // 必填
	Platform   string         `json:"platform"`              // 必填（用上方常量）
	DeviceID   string         `json:"device_id,omitempty"`
	OS         string         `json:"os,omitempty"`
	Browser    string         `json:"browser,omitempty"`
	ScreenW    int            `json:"screen_w,omitempty"`
	ScreenH    int            `json:"screen_h,omitempty"`
	Path       string         `json:"path,omitempty"`
	Title      string         `json:"title,omitempty"`
	Referrer   string         `json:"referrer,omitempty"`
	ErrorMsg   string         `json:"error_msg,omitempty"`
	ErrorStack string         `json:"error_stack,omitempty"`
	Duration   int            `json:"duration,omitempty"`
	Extra      map[string]any `json:"extra,omitempty"`
	ClientTS   int64          `json:"client_ts,omitempty"` // 客户端毫秒时间戳，为空则自动补当前时间
}

// Client 调用 AS 统计上报接口的客户端。
// 公开接口无需 app_secret 签名，只需 BaseURL + AppID（AppID 也可在 Event 级覆盖）。
type Client struct {
	BaseURL    string
	AppID      string // 默认 app_id（Event.AppID 为空时回退到此）
	HTTPClient *http.Client
}

// New 创建统计上报客户端。baseURL 指向 AS 的 /api 路由组根，不带尾斜杠
// （同 s2s 包，自 v0.8.0 起）。appID 可传空字符串，则在每条 Event 里单独指定 AppID。
func New(appID, baseURL string) *Client {
	return &Client{
		BaseURL:    trimSuffix(baseURL, "/"),
		AppID:      appID,
		HTTPClient: &http.Client{}, // Timeout=0：由方法内部 context 管超时（v0.9.0）
	}
}

// ReportEvents 批量上报统计事件（1-50 条，超出会被服务端拒绝）。
//
// 自动补全：
//   - AppID 为空 → 用 c.AppID 填充
//   - ClientTS 为 0 → 用当前毫秒时间戳填充
func (c *Client) ReportEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return fmt.Errorf("事件列表不能为空")
	}
	if len(events) > maxBatchSize {
		return fmt.Errorf("单批最多 %d 条，当前 %d 条", maxBatchSize, len(events))
	}

	// 短请求（v0.9.0）：固定 10s 超时。
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	now := time.Now().UnixMilli()
	for i := range events {
		if events[i].AppID == "" {
			events[i].AppID = c.AppID
		}
		if events[i].ClientTS == 0 {
			events[i].ClientTS = now
		}
	}

	body, _ := json.Marshal(map[string][]Event{"events": events})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+endpointEvents, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("统计上报请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("统计上报失败 (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// ReportEvent 单条事件上报的便捷封装。
func (c *Client) ReportEvent(ctx context.Context, e Event) error {
	return c.ReportEvents(ctx, []Event{e})
}

// trimSuffix 去掉结尾的指定字符串（避免引入 strings 包，保持零依赖）
func trimSuffix(s, suffix string) string {
	if len(suffix) > 0 && len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
