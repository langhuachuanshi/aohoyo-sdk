package s2s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// 默认配置
const (
	// defaultTimeout 短请求（GetUploadToken/Delete）的 context 超时。
	// v0.9.0 起 HTTPClient.Timeout=0（不再设总超时），由方法内部 context 管超时
	// （短请求用本常量，上传走 withUploadTimeout 按文件大小动态算）。详见 timeout.go。
	defaultTimeout        = 30 * time.Second
	defaultMaxUploadBytes = 100 << 20 // 与 admin-server S2S 上传上限一致：100MB
	defaultMaxConcurrency = 5         // 批量上传并发数
	defaultRatePerSec     = 9         // 批量上传每秒请求数上限（AS S2S 限流 600/min=10/s，留余量取 9）
	defaultMaxRetries     = 3         // 429 自动重试次数
)

// Client 调用 admin-server 对外存储接口（/storage/*）的 S2S 客户端。
//
// 同一套 /storage/* 接口（AS 内部挂 engine.Group("/api/storage")）同时支持
// JWT（SDK 用户）和 S2S 签名（外部服务）双鉴权，本客户端用 S2S 签名身份调用。
// 三要素：BaseURL + AppID + AppSecret（apps.app_secret）。所有方法自动签名，调用方无需关心 header 拼装。
//
// BaseURL 契约（v0.8.0 起）：永远指向「admin-server 的 /api 路由组根」，
// 本客户端只拼 /storage/* 到具体接口，不再含 /api 前缀：
//   - 容器内直连：BaseURL = http://aohoyo-as:33800/api
//   - 外网经反代：BaseURL = https://api.example.com（Caddy 兜底补 /api，等价于上面）
// 这样把 /api 前缀的责任集中到 BaseURL 一处，避免经反代时出现 /api/api/... 双前缀 404。
type Client struct {
	BaseURL    string // /api 路由组根，例：http://host:33800/api 或 https://api.example.com（不带尾斜杠）
	AppID      string
	AppSecret  string
	HTTPClient *http.Client

	// MaxUploadBytes 上传大小上限（默认 100MB，与服务端一致）
	MaxUploadBytes int64

	// MaxConcurrency 批量上传的并发 worker 数（默认 5）
	MaxConcurrency int

	// RatePerSec 批量上传的每秒请求数上限（默认 9）
	// AS 的 S2S 限流是 600/min = 10/s，留 1 个余量取 9。
	// 超过会被服务端 429，sdk 会自动退避重试（见 MaxRetries）。
	RatePerSec int

	// MaxRetries 单文件上传遇到 429（限流）时的自动重试次数（默认 3）
	// 每次重试间隔 = 2^retryCount 秒（指数退避），或读 Retry-After 头。
	MaxRetries int
}

// New 创建一个 S2S 客户端。baseURL 指向 AS 的 /api 路由组根，不带尾斜杠，
// 例如 "http://aohoyo-as:33800/api"（容器内直连）或 "https://api.example.com"（外网经反代）。
// 自 v0.8.0 起 BaseURL 语义变更：不再由 SDK 拼 /api 前缀，调用方需自行带上 /api（外网经反代除外）。
func New(appID, appSecret, baseURL string) *Client {
	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		AppID:          appID,
		AppSecret:      appSecret,
		HTTPClient:     &http.Client{}, // Timeout=0：由各方法内部 context 管超时（v0.9.0）
		MaxUploadBytes: defaultMaxUploadBytes,
		MaxConcurrency: defaultMaxConcurrency,
		RatePerSec:     defaultRatePerSec,
		MaxRetries:     defaultMaxRetries,
	}
}

// UploadResult 上传响应（剥离 {code,data} 包装后的 data 部分）
type UploadResult struct {
	URL      string `json:"url"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}

// UploadTokenResult 上传凭证响应（服务商无关格式）
//
// Mode="direct" 时读 Upload 字段按 Method 分派直传；
// Mode="proxy" 时 Upload 为空，调用方回退 Upload() 代理。
type UploadTokenResult struct {
	Mode     string           `json:"mode"`               // "direct" / "proxy"
	Provider string           `json:"provider,omitempty"` // qiniu / aliyun / tencent / local
	Key      string           `json:"key,omitempty"`      // 对象存储 key
	URL      string           `json:"url,omitempty"`      // 上传成功后的访问 URL
	Upload   *UploadDirective `json:"upload,omitempty"`   // 直传指令（mode=direct 时必有）

	// 旧字段（AS 向后兼容返回，新代码用 Upload.Fields["token"])
	Token  string `json:"token,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// UploadDirective 直传指令：告诉调用方怎么把文件字节传到对象存储。
//
// 调用方按 Method 分派：
//   - POST：构造 multipart（Fields + FileField + 文件），POST 到 Endpoint
//   - PUT：文件字节作为 body，带 Headers，PUT 到 Endpoint（未来 OSS/COS）
//
// 批量模式（KeyPrefix 非空）：Fields 不含 key，sdk 每文件拼 KeyPrefix+相对路径作为 key。
type UploadDirective struct {
	Method    string            `json:"method"`               // "POST" / "PUT"
	Endpoint  string            `json:"endpoint"`             // 上传端点 URL
	Fields    map[string]string `json:"fields,omitempty"`     // 表单字段（POST 用，批量模式不含 key）
	Headers   map[string]string `json:"headers,omitempty"`    // 额外请求头（PUT 用）
	FileField string            `json:"file_field,omitempty"` // 文件字段名（POST 用，默认 "file"）
	KeyPrefix string            `json:"key_prefix,omitempty"` // 对象 key 前缀（批量模式用，sdk 拼 key）
}

// apiResp admin-server 统一响应壳
// 注意：AS 成功返回 {"code":200,"message":"success","data":{...}}，
// 不是 code:0；字段名是 message 不是 msg。
type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"message"`
	Data json.RawMessage `json:"data"`
}

// Upload 通过 S2S 接口上传文件到 admin-server（走服务端代理上传）。
//
//	pathPrefix：存储目录前缀，如 "uploads/"、"wilas/files/"（空则服务端默认 "uploads/"）
//	fileName：  原始文件名（用于取扩展名）
//	data：      文件内容字节
func (c *Client) Upload(ctx context.Context, pathPrefix, fileName string, data []byte) (*UploadResult, error) {
	if int64(len(data)) > c.MaxUploadBytes {
		return nil, fmt.Errorf("文件大小 %d 超过上限 %d", len(data), c.MaxUploadBytes)
	}

	// 按文件大小动态算超时（v0.9.0）：大文件代理上传不再被固定 30s 切断。
	ctx, cancel := withUploadTimeout(ctx, int64(len(data)))
	defer cancel()

	// 构造 multipart body（带签名）。
	// 注意：multipart 的边界每次不同，body 内容不稳定 → 必须先构造完 body 再签名。
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 写 path 字段
	if pathPrefix != "" {
		if err := writer.WriteField("path", pathPrefix); err != nil {
			return nil, fmt.Errorf("写入 path 字段失败: %w", err)
		}
	}

	// 写 file 字段
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, fmt.Errorf("创建 file part 失败: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/storage/upload", bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for k, v := range SignHeaders(c.AppID, c.AppSecret, body.Bytes()) {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("S2S 上传请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("S2S 上传失败 (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("S2S 上传业务错误: %s", r.Msg)
	}

	var out UploadResult
	if err := json.Unmarshal(r.Data, &out); err != nil {
		return nil, fmt.Errorf("解析上传结果失败: %w", err)
	}
	return &out, nil
}

// GetUploadToken 获取直传凭证（服务商无关）。
//
// mode 决定凭证类型：
//   - "" 或 "key"：单 key 凭证（objectKey 指定文件路径，空则 AS 自动重命名）
//   - "prefix"：批量凭证（1 个凭证传整个目录，七牛 bucket scope）
//
// 非七牛服务商返回 Mode="proxy"，调用方需改走 Upload()。
func (c *Client) GetUploadToken(ctx context.Context, pathPrefix, fileName, objectKey, mode string) (*UploadTokenResult, error) {
	// 短请求（v0.9.0）：固定 30s 超时，凭证 body 很小。
	ctx, cancel := withShortTimeout(ctx)
	defer cancel()

	payload := map[string]string{
		"path":     pathPrefix,
		"filename": fileName,
	}
	if objectKey != "" {
		payload["key"] = objectKey
	}
	if mode != "" {
		payload["mode"] = mode
	}
	reqBody, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/storage/upload-token", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range SignHeaders(c.AppID, c.AppSecret, reqBody) {
		req.Header.Set(k, v)
	}

	raw, err := c.doAndUnwrap(ctx, req)
	if err != nil {
		return nil, err
	}
	var out UploadTokenResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析上传凭证失败: %w", err)
	}
	return &out, nil
}

// Delete 批量删除文件。paths 是对象存储 key 列表（不含域名）。
func (c *Client) Delete(ctx context.Context, paths []string) error {
	// 短请求（v0.9.0）：固定 30s 超时。
	ctx, cancel := withShortTimeout(ctx)
	defer cancel()

	reqBody, _ := json.Marshal(map[string][]string{"paths": paths})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/v1/storage/delete", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range SignHeaders(c.AppID, c.AppSecret, reqBody) {
		req.Header.Set(k, v)
	}

	_, err = c.doAndUnwrap(ctx, req)
	return err
}

// doAndUnwrap 执行请求 + 校验状态码 + 剥离 {code,data} 外壳，返回 data 原始字节。
func (c *Client) doAndUnwrap(ctx context.Context, req *http.Request) ([]byte, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("S2S 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("S2S 请求失败 (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("S2S 业务错误: %s", r.Msg)
	}
	return r.Data, nil
}

// String 方便调试
func (r *UploadResult) String() string {
	return fmt.Sprintf("UploadResult{URL: %s, Size: %d}", r.URL, r.FileSize)
}
