// Package aohoyo 提供 aohoyo 平台服务端 SDK 的统一入口。
//
// 一次初始化，按需调用各能力子包。内部组合 s2s（存储）、stats（统计）、
// uc（用户中心），避免调用方重复传 appID/baseURL 初始化多个客户端。
//
// 最小示例：
//
//	c, err := aohoyo.New(appID, appSecret, "https://api.aohoyo.com")
//	if err != nil { panic(err) }
//
//	// 存储操作（自动 S2S 签名）
//	res, err := c.S2S.Upload(ctx, "files/", "test.zip", data)
//
//	// 统计上报（公开接口）
//	err = c.Stats.ReportEvent(ctx, stats.Event{...})
//
//	// 用户中心操作（Bearer token 透传）
//	identity, err := c.UC.VerifyToken(ctx, userToken)
//
// 子包 s2s / stats / uc 仍可独立使用（向后兼容，已发布版本不受影响）。
package aohoyo

import (
	"fmt"
	"net/http"
	"time"

	"github.com/langhuachuanshi/aohoyo-sdk/go/s2s"
	"github.com/langhuachuanshi/aohoyo-sdk/go/stats"
	"github.com/langhuachuanshi/aohoyo-sdk/go/uc"
)

// 默认配置
const (
	// defaultTimeout 保留给可能的顶层短请求超时；v0.9.0 起 hc.Timeout=0，
	// 实际超时由 s2s/stats 子包各方法内部 context 管（详见子包 timeout.go）。
	defaultTimeout = 30 * time.Second
)

// Client 是 aohoyo 平台服务端 SDK 的统一客户端。
//
// 一次初始化拿到所有能力：
//   - S2S：存储上传/删除（需 app_secret 签名）
//   - Stats：统计事件上报（公开接口）
//   - UC：用户中心 Token 验证 / 用户查询（Bearer token 透传）
//
// 未来新增能力（如通知、应用配置查询等）会作为新字段加入本结构。
type Client struct {
	AppID     string
	AppSecret string
	BaseURL   string

	// S2S 存储操作客户端（上传/删除/直传凭证）
	S2S *s2s.Client

	// Stats 统计上报客户端
	Stats *stats.Client

	// UC 用户中心客户端（token 验证 / 用户查询）
	UC *uc.Client
}

// New 创建统一客户端。baseURL 不带尾斜杠。
//
// appSecret 用于 S2S 签名，不能为空（统计上报虽不需要签名，
// 但统一客户端要求应用身份完整，便于未来扩展需鉴权的能力）。
func New(appID, appSecret, baseURL string) (*Client, error) {
	if appID == "" {
		return nil, fmt.Errorf("appID 不能为空")
	}
	if appSecret == "" {
		return nil, fmt.Errorf("appSecret 不能为空")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL 不能为空")
	}

	// hc.Timeout=0（v0.9.0）：不再由 HTTP client 设总超时，改由 s2s/stats 各方法
	// 内部 context 按请求类型管超时（短请求 30s/10s，上传按文件大小动态算）。
	// 复用同一连接池的语义不变，Transport 由 http.DefaultTransport 提供。
	httpClient := &http.Client{}

	return &Client{
		AppID:     appID,
		AppSecret: appSecret,
		BaseURL:   baseURL,
		S2S:       newS2SClient(appID, appSecret, baseURL, httpClient),
		Stats:     newStatsClient(appID, baseURL, httpClient),
		UC:        newUCClient(baseURL, httpClient),
	}, nil
}

// newS2SClient 构造 S2S 客户端，复用统一 HTTP 连接池
func newS2SClient(appID, appSecret, baseURL string, hc *http.Client) *s2s.Client {
	c := s2s.New(appID, appSecret, baseURL)
	c.HTTPClient = hc
	return c
}

// newStatsClient 构造 Stats 客户端，复用统一 HTTP 连接池
func newStatsClient(appID, baseURL string, hc *http.Client) *stats.Client {
	c := stats.New(appID, baseURL)
	c.HTTPClient = hc
	return c
}

// newUCClient 构造 UC 客户端，复用统一 HTTP 连接池
func newUCClient(baseURL string, hc *http.Client) *uc.Client {
	c := uc.New(baseURL)
	c.HTTPClient = hc
	return c
}
