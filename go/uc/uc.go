// Package uc 提供 aohoyo user-center 的 S2S 调用客户端。
//
// 对应 UC 的 JWT 鉴权接口，用于接入方后端验证用户 token、查询用户信息。
// 鉴权方式：透传终端用户的 Bearer token（不是 app_secret S2S 签名）。
//
// 覆盖三个标准 S2S 场景：
//   - VerifyToken：admin 鉴权中间件，验证 token 拿身份+权限
//   - ListUsers：批量翻译 user_id → username
//   - SearchUsers：按用户名/手机号搜索用户
//
// BaseURL 契约（同 s2s/stats 包）：永远指向「user-center 的 /api 路由组根」，
// 本包只拼 /v1/auth/verify、/v1/users 等具体路径：
//   - 容器内直连：BaseURL = http://aohoyo-uc:33700/api
//   - 外网经反代：BaseURL = https://api.aohoyo.com（Caddy 补 /api 前缀）
//
// 最小示例：
//
//	c := uc.New("https://api.aohoyo.com")
//	identity, err := c.VerifyToken(ctx, userToken)
//	users, err := c.SearchUsers(ctx, adminToken, "testuser")
package uc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultTimeout 短请求的 context 超时。
	// HTTPClient.Timeout=0（不设总超时），由方法内部 context 管超时。
	defaultTimeout = 30 * time.Second

	// 端点路径（BaseURL 指向 /api 路由组根）
	endpointVerify = "/v1/auth/verify"
	endpointUsers  = "/v1/users"

	// 搜索兜底分页大小
	searchPageSize = 20
)

// UserIdentity VerifyToken 的返回结果。
type UserIdentity struct {
	UserID      int64    `json:"user_id"`
	Username    string   `json:"username"`
	RoleCodes   []string `json:"role_codes"`
	Permissions []string `json:"permissions"`
}

// User 用户列表项，字段与 UC users 表对齐。
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	UserCode  string `json:"user_code"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar"`
	Status    int16  `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListUsersOptions 用户列表查询参数。
type ListUsersOptions struct {
	Page     int    // 页码，默认 1
	PageSize int    // 每页条数，默认 10，最大 100
	Username string // 按用户名模糊搜索
	Phone    string // 按手机号模糊搜索
	UserCode string // 按社交号模糊搜索
	Status   *int16 // 按状态筛选
}

// UserListResult 用户列表分页结果。
type UserListResult struct {
	List     []User `json:"list"`
	Total    int64  `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

// Client 调用 user-center JWT 鉴权接口的客户端。
//
// 注意：本客户端使用 Bearer token 鉴权（透传用户 token），不使用 app_secret S2S 签名。
// 这与 s2s 包不同——verify/users 接口设计为接入方后端代替前端调用 UC，
// 鉴权依据是用户自己的 JWT，而不是接入方应用的 app_secret。
type Client struct {
	BaseURL    string       // /api 路由组根，例：http://host:33700/api 或 https://api.aohoyo.com
	HTTPClient *http.Client // 共享连接池（默认 http.DefaultClient 式）
}

// New 创建 UC 客户端。baseURL 不带尾斜杠。
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{}, // Timeout=0：由方法内部 context 管超时
	}
}

// ucResp user-center 统一响应壳（与 AS 的 code/message/data 一致）。
type ucResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"message"`
	Data json.RawMessage `json:"data"`
}

// VerifyToken 验证 Bearer token 有效性，返回用户身份与权限信息。
//
// 对应 UC 接口：POST /api/v1/auth/verify（JWT 鉴权）
//
// 用途：接入方的 admin 鉴权中间件。每次 /admin 请求到来时，
// 把前端传来的 Bearer token 透传给 UC 验证，拿到 user_id / username /
// role_codes / permissions 后注入请求上下文。
//
// token 是终端用户的 access_token（Bearer 之后的字符串，不含 "Bearer " 前缀）。
func (c *Client) VerifyToken(ctx context.Context, token string) (*UserIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+endpointVerify, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 verify 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	data, err := c.doAndUnwrap(req)
	if err != nil {
		return nil, fmt.Errorf("verify token 失败: %w", err)
	}

	var out UserIdentity
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 verify 响应失败: %w", err)
	}
	return &out, nil
}

// ListUsers 分页查询用户列表。
//
// 对应 UC 接口：GET /api/v1/users（JWT + user:list 权限）
//
// 用途：admin 后台把 user_id 列表翻译成可读的 username。
// token 需要有 user:list 权限（通常用 admin 服务账号的 token）。
func (c *Client) ListUsers(ctx context.Context, token string, opts ListUsersOptions) (*UserListResult, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	// 构造查询参数
	q := url.Values{}
	if opts.Page > 0 {
		q.Set("page", fmt.Sprintf("%d", opts.Page))
	}
	if opts.PageSize > 0 {
		if opts.PageSize > 100 {
			opts.PageSize = 100
		}
		q.Set("page_size", fmt.Sprintf("%d", opts.PageSize))
	}
	if opts.Username != "" {
		q.Set("username", opts.Username)
	}
	if opts.Phone != "" {
		q.Set("phone", opts.Phone)
	}
	if opts.UserCode != "" {
		q.Set("user_code", opts.UserCode)
	}
	if opts.Status != nil {
		q.Set("status", fmt.Sprintf("%d", *opts.Status))
	}

	reqURL := c.BaseURL + endpointUsers
	if len(q) > 0 {
		reqURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 list users 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	data, err := c.doAndUnwrap(req)
	if err != nil {
		return nil, fmt.Errorf("list users 失败: %w", err)
	}

	var out UserListResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("解析 list users 响应失败: %w", err)
	}
	return &out, nil
}

// SearchUsers 按关键词搜索用户。
//
// 搜索策略：先按 username 查，无结果再按 phone 查（兜底）。
// 这是 090cq admin_yuanbao.go SearchUsers 的逻辑封装到 SDK。
//
// 对应 UC 接口：GET /api/v1/users?username=<kw> 和 /api/v1/users?phone=<kw>
func (c *Client) SearchUsers(ctx context.Context, token string, keyword string) ([]User, error) {
	if keyword == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}

	// 先按 username 搜
	result, err := c.ListUsers(ctx, token, ListUsersOptions{
		Page:     1,
		PageSize: searchPageSize,
		Username: keyword,
	})
	if err != nil {
		return nil, fmt.Errorf("search users (username) 失败: %w", err)
	}
	if len(result.List) > 0 {
		return result.List, nil
	}

	// username 没命中，按 phone 兜底
	result, err = c.ListUsers(ctx, token, ListUsersOptions{
		Page:     1,
		PageSize: searchPageSize,
		Phone:    keyword,
	})
	if err != nil {
		return nil, fmt.Errorf("search users (phone) 失败: %w", err)
	}
	return result.List, nil
}

// doAndUnwrap 执行请求 + 校验状态码 + 剥离 {code,data} 外壳，返回 data 原始字节。
func (c *Client) doAndUnwrap(req *http.Request) ([]byte, error) {
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("UC 请求失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 UC 响应失败: %w", err)
	}

	// HTTP 401 = token 无效或过期
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("token 无效或过期 (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("UC 请求失败 (HTTP %d): %s", resp.StatusCode, string(raw))
	}

	var r ucResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("解析 UC 响应失败: %w", err)
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("UC 业务错误: %s", r.Msg)
	}
	return r.Data, nil
}
