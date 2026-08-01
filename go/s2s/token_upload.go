package s2s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// UploadByToken 凭证直传：先拿凭证，再按 AS 返回的指令直传到对象存储。
//
// objectKey 为可选完整 key（如 "docs/教程/index.html"），传空则 AS 按日期/纳秒自动生成。
//
// 对比 Upload（走 AS 代理）：文件字节不经 AS，直接传到对象存储（七牛/OSS/COS）。
// 适合大文件或批量上传场景，省 AS 带宽。
//
// 流程：
//  1. POST /storage/upload-token 拿凭证（带 S2S 签名；BaseURL 已含 /api）
//  2. mode=proxy → 回退 c.Upload() 代理上传（本地存储等）
//  3. mode=direct → 按 upload.Method 直传到对象存储（不经 AS）：
//     - POST：表单上传（七牛：Fields.token + Fields.key + 文件）
//     - PUT：预签名 URL（未来 OSS/COS，本次未实现）
//
// 返回的 URL 来自凭证响应（AS 生成 objectKey 时算好的），不是对象存储返回的。
func (c *Client) UploadByToken(ctx context.Context, pathPrefix, fileName, objectKey string, data []byte) (*UploadResult, error) {
	if int64(len(data)) > c.MaxUploadBytes {
		return nil, fmt.Errorf("文件大小 %d 超过上限 %d", len(data), c.MaxUploadBytes)
	}

	// ① 拿凭证（单 key 模式）
	tok, err := c.GetUploadToken(ctx, pathPrefix, fileName, objectKey, "")
	if err != nil {
		return nil, fmt.Errorf("获取上传凭证失败: %w", err)
	}

	// ② proxy 模式：回退代理上传
	if tok.Mode != "direct" || tok.Upload == nil {
		return c.Upload(ctx, pathPrefix, fileName, data)
	}

	// ③ direct 模式：按 method 分派直传
	url, err := c.uploadByDirective(ctx, tok, data, fileName)
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		URL:      url,
		FileName: fileName,
		FileSize: int64(len(data)),
	}, nil
}

// uploadByDirective 按 AS 返回的 UploadDirective 直传文件字节到对象存储。
// 返回最终可访问的 URL（来自凭证响应的 tok.URL）。
func (c *Client) uploadByDirective(ctx context.Context, tok *UploadTokenResult, data []byte, fileName string) (string, error) {
	d := tok.Upload
	if d == nil {
		return "", fmt.Errorf("凭证缺少 upload 指令")
	}

	switch d.Method {
	case "POST":
		return c.directPOST(ctx, d, data, fileName, tok.URL, "") // 单 key 模式：用 Fields["key"]
	case "PUT":
		// 未来 OSS/COS 预签名 URL 直传，本次未实现
		return "", fmt.Errorf("PUT 直传暂未实现（provider=%s），请用 Upload() 代理", tok.Provider)
	default:
		return "", fmt.Errorf("不支持的直传方法: %s", d.Method)
	}
}

// directPOST 表单 POST 直传（七牛：token + key + file → upload.qiniup.com）
//
// 注意：POST 到对象存储端点不带 S2S 签名！
// 对象存储用自己的鉴权（七牛的 token 自带上传凭证，已含 scope 签名）。
// directPOST 表单 POST 直传。
// overrideKey 非空时覆盖 Fields["key"]（批量模式每文件不同 key），空则用 Fields["key"]。
func (c *Client) directPOST(ctx context.Context, d *UploadDirective, data []byte, fileName, expectedURL, overrideKey string) (string, error) {
	fileField := d.FileField
	if fileField == "" {
		fileField = "file"
	}

	// 按文件大小动态算超时（v0.9.0），包在整个重试循环外：
	// 大文件直传不再被固定 30s 切断导致重试死循环；总时长超时则整个方法失败。
	ctx, cancel := withUploadTimeout(ctx, int64(len(data)))
	defer cancel()

	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = defaultMaxRetries
	}

	var lastErr error
	var retryAfter time.Duration

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			if retryAfter > 0 && retryAfter < 60*time.Second {
				backoff = retryAfter
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", fmt.Errorf("直传 %s 已取消: %w", fileName, ctx.Err())
			}
		}

		// 每次重试都重新构造 multipart（body 内容稳定但 reader 只能读一次）
		body, contentType, err := buildDirectPOSTBody(d, fileField, fileName, data, overrideKey)
		if err != nil {
			return "", err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", contentType)
		// 额外请求头（七牛通常不需要，OSS/COS POST 模式可能需要）
		for k, v := range d.Headers {
			req.Header.Set(k, v)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("直传 %s 失败: %w", fileName, err)
			continue
		}

		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("读取 %s 直传响应失败: %w", fileName, readErr)
			continue
		}

		// 429 限流：退避重试
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			lastErr = fmt.Errorf("直传 %s 被限流 (HTTP 429)，第 %d 次重试", fileName, attempt+1)
			continue
		}

		// 5xx 服务端错误：退避重试
		if resp.StatusCode >= 500 && attempt < maxRetries {
			lastErr = fmt.Errorf("直传 %s 服务端错误 (HTTP %d)，第 %d 次重试", fileName, resp.StatusCode, attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("直传 %s 失败 (HTTP %d): %s", fileName, resp.StatusCode, string(raw))
		}

		// 成功：返回凭证里的 URL（对象存储返回的 hash/key 不含访问域名）
		return expectedURL, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("直传 %s 失败（已重试 %d 次）: %w", fileName, maxRetries, lastErr)
	}
	return "", fmt.Errorf("直传 %s 失败（未知原因）", fileName)
}

// buildDirectPOSTBody 构造对象存储直传的 multipart 表单
//
// 格式（七牛）：
//
//	--boundary
//	Content-Disposition: form-data; name="token"
//
//	<token 值>
//	--boundary
//	Content-Disposition: form-data; name="key"
//
//	<key 值>
//	--boundary
//	Content-Disposition: form-data; name="file"; filename="<fileName>"
//	Content-Type: application/octet-stream
//
//	<文件字节>
//	--boundary--
// overrideKey 非空时，写入 multipart 的 key 字段用 overrideKey（批量模式每文件不同 key），
// 否则用 d.Fields["key"]（单 key 模式）。
func buildDirectPOSTBody(d *UploadDirective, fileField, fileName string, data []byte, overrideKey string) ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 先写 Fields（七牛要求 token 和 key 在 file 之前）
	for k, v := range d.Fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", fmt.Errorf("写入字段 %s 失败: %w", k, err)
		}
	}
	// 批量模式：overrideKey 覆盖 key 字段（Fields 里没有 key，这里补上）
	if overrideKey != "" {
		if err := writer.WriteField("key", overrideKey); err != nil {
			return nil, "", fmt.Errorf("写入 key 字段失败: %w", err)
		}
	}

	// 写文件（字段名 = fileField）
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fileField, fileName)}
	header["Content-Type"] = []string{"application/octet-stream"}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("创建 file part 失败: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", fmt.Errorf("写入文件内容失败: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

// UploadBatchByToken 凭证批量直传：并发拿凭证 + 直传对象存储，带进度 + 429 重试。
//
// 对比 UploadBatch（走 AS 代理）：文件字节不经 AS，直传对象存储，省 AS 带宽。
// 每个文件独立拿凭证（七牛 token scope 锁单 key，不可复用）。
//
// 内部复用 uploadBatchGeneric 的 worker 池 + 节流 + 进度逻辑，
// singleFn 换成 uploadSingleByToken（凭证直传）。
//
// mode=proxy 的文件会自动回退到代理上传（本地存储等）。
func (c *Client) UploadBatchByToken(ctx context.Context, items []UploadItem, onProgress func(done, total int), opts ...BatchOption) (*BatchResult, error) {
	return c.uploadBatchGeneric(ctx, items, onProgress, c.uploadSingleByToken, opts...)
}

// uploadSingleByToken 单文件凭证直传（供批量引擎调用）。
//
// 流程：拿凭证 → mode 判断 → direct 直传 / proxy 回退 uploadSingle。
// 429/5xx 退避重试由 directPOST 内部处理（复用 MaxRetries）。
// 注意：拿凭证（/storage/upload-token）本身也可能 429，这里做一次重试。
func (c *Client) uploadSingleByToken(ctx context.Context, item UploadItem) (string, error) {
	if int64(len(item.Data)) > c.MaxUploadBytes {
		return "", fmt.Errorf("文件 %s 大小 %d 超过上限 %d", item.FileName, len(item.Data), c.MaxUploadBytes)
	}

	// ① 拿凭证（带重试，凭证接口也可能 429）
	// item.Key 非空时传完整 key（CHM 保持目录结构），空则 AS 默认重命名
	tok, err := c.getUploadTokenWithRetry(ctx, item.PathPrefix, item.FileName, item.Key, "")
	if err != nil {
		return "", fmt.Errorf("获取 %s 凭证失败: %w", item.FileName, err)
	}

	// ② proxy 模式：回退代理上传（复用 uploadSingle 的签名 + 重试逻辑）
	if tok.Mode != "direct" || tok.Upload == nil {
		return c.uploadSingle(ctx, item)
	}

	// ③ direct 模式：按指令直传
	return c.uploadByDirective(ctx, tok, item.Data, item.FileName)
}

// getUploadTokenWithRetry 拿凭证，带 429 退避重试（凭证接口也受 AS 限流保护）
func (c *Client) getUploadTokenWithRetry(ctx context.Context, pathPrefix, fileName, objectKey, mode string) (*UploadTokenResult, error) {
	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = defaultMaxRetries
	}

	var lastErr error
	var retryAfter time.Duration

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			if retryAfter > 0 && retryAfter < 60*time.Second {
				backoff = retryAfter
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, fmt.Errorf("已取消: %w", ctx.Err())
			}
		}

		tok, err := c.GetUploadToken(ctx, pathPrefix, fileName, objectKey, mode)
		if err == nil {
			return tok, nil
		}

		lastErr = err
		// 凭证接口的错误信息里可能含 "429"，简单退避重试
		if attempt < maxRetries {
			continue
		}
	}

	return nil, fmt.Errorf("已重试 %d 次: %w", maxRetries, lastErr)
}

// UploadBatchByPrefix 前缀凭证批量直传：1 个凭证传整个目录（CHM 整站场景）。
//
// 对比 UploadBatchByToken（每文件 1 个凭证）：只拿 1 次 bucket 级凭证，
// 900 文件从 900 次拿凭证降到 1 次，从 30 分钟降到 < 2 分钟。
//
// 流程：
//  1. 调 GetUploadToken(mode=prefix) 拿 1 个 bucket 级凭证（带 S2S 签名）
//  2. mode=proxy → 回退 UploadBatch（逐文件代理，本地存储等）
//  3. mode=direct → 把每文件的完整 key 写回 item.Key，复用 uploadBatchGeneric
//     singleFn 用同一凭证 + item.Key 作为 overrideKey 直传
//
// key 拼接：item.Key 非空用 item.Key，否则用 KeyPrefix + item.FileName。
// 例如 KeyPrefix="docs/教程/", item.FileName="content/page.html" → key="docs/教程/content/page.html"
//
// 七牛 bucket scope 允许覆盖：重新上传同站点会覆盖旧文件。
// 直传不经 AS，不受 AS 限流约束，并发可放开。
func (c *Client) UploadBatchByPrefix(ctx context.Context, keyPrefix string, items []UploadItem, onProgress func(done, total int), opts ...BatchOption) (*BatchResult, error) {
	if len(items) == 0 {
		return &BatchResult{URLs: []string{}, Errors: []error{}}, nil
	}

	// ① 拿 1 个 bucket 级凭证（带重试）
	tok, err := c.getUploadTokenWithRetry(ctx, keyPrefix, "", "", "prefix")
	if err != nil {
		return nil, fmt.Errorf("获取批量凭证失败: %w", err)
	}

	// ② proxy 模式：回退逐文件代理上传
	if tok.Mode != "direct" || tok.Upload == nil {
		return c.UploadBatch(ctx, items, onProgress)
	}

	// ③ direct 模式：给每个 item 填完整 key（worker 里用 item.Key 作为 overrideKey）
	prefixKey := tok.Upload.KeyPrefix
	if prefixKey == "" {
		prefixKey = keyPrefix
	}
	baseURL := tok.URL
	for i := range items {
		if items[i].Key == "" {
			items[i].Key = prefixKey + items[i].FileName
		}
	}

	// singleFn：用同一凭证 + item.Key 作为 overrideKey 直传
	// closure 捕获 tok，所有 worker 复用同一个凭证
	singleFn := func(ctx context.Context, item UploadItem) (string, error) {
		if int64(len(item.Data)) > c.MaxUploadBytes {
			return "", fmt.Errorf("文件 %s 大小 %d 超过上限 %d", item.FileName, len(item.Data), c.MaxUploadBytes)
		}
		// item.Key 已在前面填好（完整 key），finalURL 用 baseURL + FileName
		finalURL := baseURL + item.FileName
		return c.directPOST(ctx, tok.Upload, item.Data, item.FileName, finalURL, item.Key)
	}

	return c.uploadBatchGeneric(ctx, items, onProgress, singleFn, opts...)
}
