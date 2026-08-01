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

// progressReader 包装 io.Reader，每次 Read 累计已读字节并回调 onProgress。
//
// 用于流式上传：HTTP client 从网络读走多少字节，pipe 才放行多少字节，
// Read 被调用 = 网络真实传输出进度，不是「读到内存」的进度。
// onProgress 可为 nil（Read 照常工作，不回调）。
type progressReader struct {
	r          io.Reader
	uploaded   int64
	total      int64
	onProgress func(uploaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.uploaded += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(pr.uploaded, pr.total)
		}
	}
	return n, err
}

// UploadByTokenWithProgress 凭证直传 + 字节级进度回调，语义与 UploadByToken 一致，
// 仅多了 onProgress。用于 workbench 一键上传等需要显示百分比的场景。
//
// onProgress(uploaded, total int64)：total=len(data)，仅在网络传输阶段触发
// （非「读到内存」），uploaded 单调递增、最终等于 total。
//
// 兼容性：
//   - onProgress==nil：直接转调 UploadByToken，行为字节级一致（向后兼容）。
//   - proxy 模式（tok.Mode != "direct"）：回退 Upload 代理上传，无进度回调
//     （本地存储上传快，可接受）。
//   - PUT 模式（OSS/COS 预签名）：暂未实现，返回错误（同 UploadByToken）。
func (c *Client) UploadByTokenWithProgress(ctx context.Context, pathPrefix, fileName, objectKey string,
	data []byte, onProgress func(uploaded, total int64)) (*UploadResult, error) {

	// onProgress==nil：与原 UploadByToken 完全一致（字节级）
	if onProgress == nil {
		return c.UploadByToken(ctx, pathPrefix, fileName, objectKey, data)
	}

	if int64(len(data)) > c.MaxUploadBytes {
		return nil, fmt.Errorf("文件大小 %d 超过上限 %d", len(data), c.MaxUploadBytes)
	}

	// ① 拿凭证（单 key 模式）
	tok, err := c.GetUploadToken(ctx, pathPrefix, fileName, objectKey, "")
	if err != nil {
		return nil, fmt.Errorf("获取上传凭证失败: %w", err)
	}

	// ② proxy 模式：回退代理上传（本地存储等，无进度回调）
	if tok.Mode != "direct" || tok.Upload == nil {
		return c.Upload(ctx, pathPrefix, fileName, data)
	}

	// ③ direct 模式：按 method 分派直传
	d := tok.Upload
	var url string
	switch d.Method {
	case "POST":
		// 单 key 模式：用 Fields["key"]（overrideKey 留空，与 UploadByToken 一致）
		url, err = c.directPOSTStream(ctx, d, data, fileName, tok.URL, "", onProgress)
	case "PUT":
		// 未来 OSS/COS 预签名 URL 直传，本次未实现
		err = fmt.Errorf("PUT 直传暂未实现（provider=%s），请用 Upload() 代理", tok.Provider)
	default:
		err = fmt.Errorf("不支持的直传方法: %s", d.Method)
	}
	if err != nil {
		return nil, err
	}

	return &UploadResult{
		URL:      url,
		FileName: fileName,
		FileSize: int64(len(data)),
	}, nil
}

// directPOSTStream 表单 POST 直传的流式变体（带进度回调）。
//
// 与 directPOST 的区别：body 构造从「buildDirectPOSTBody 拼成 []byte 再 bytes.NewReader」
// 改为「io.Pipe + multipart.NewWriter 流式」，文件字段经 progressReader 包装，
// HTTP client 边读边发，进度随网络传输实时回调。
//
// 重试逻辑与 directPOST 一致（429/5xx 退避重试）。由于 io.Pipe 的 reader 只能读一次，
// 每次重试都重新开 pipe + 重起 goroutine 写 multipart（body 内容稳定，boundary 每次不同）。
//
// overrideKey 非空时覆盖 Fields["key"]（批量模式每文件不同 key），空则用 Fields["key"]。
// 当前只被单文件 UploadByTokenWithProgress 调用，overrideKey 恒为 ""。
func (c *Client) directPOSTStream(ctx context.Context, d *UploadDirective, data []byte,
	fileName, expectedURL, overrideKey string, onProgress func(uploaded, total int64)) (string, error) {

	fileField := d.FileField
	if fileField == "" {
		fileField = "file"
	}

	// 按文件大小动态算超时（v0.9.0），包在整个重试循环外：
	// 大文件流式直传不再被固定 30s 切断；io.Pipe 每次重试重新开，超时则整个方法失败。
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

		// 每次重试都重新开 pipe（reader 只能读一次）
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)

		// goroutine 写 multipart body：HTTP client 读 pipe 时被驱动执行。
		// 写完关闭 mw（写尾 boundary），再关 pw（通知 client body 结束）。
		// 任何写错经 pw.CloseWithError 传给 reader，避免静默失败。
		go func() {
			writeErr := writeStreamBody(mw, pw, d, fileField, fileName, data, overrideKey, onProgress)
			if writeErr != nil {
				pw.CloseWithError(writeErr) // 写半截时通知 reader 读到错误
				return
			}
			pw.Close() // 正常结束
		}()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, pr)
		if err != nil {
			pr.Close() // 放弃 pipe（goroutine 写到 Close 自动结束）
			return "", err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType()) // boundary 每次不同，取当前 mw 的
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

// writeStreamBody 用 multipart.Writer 把直传表单流式写到 w（通常是 io.Pipe 的 writer 端）。
//
// 写入顺序与 buildDirectPOSTBody 一致：
//  1. Fields（七牛要求 token/key 在 file 之前）
//  2. overrideKey（批量模式每文件不同 key，单文件模式为空不写）
//  3. file part：file 字段名 + 文件名，内容经 progressReader 包装（读多少字节回调多少）
//  4. Close writer（写尾 boundary）
//
// 返回首个写错（用于 pipe.CloseWithError 通知 reader）。mw.Close 总会执行：
// 正常时写尾 boundary 后返回 nil，写错时返回该错误。
func writeStreamBody(mw *multipart.Writer, w io.Writer, d *UploadDirective,
	fileField, fileName string, data []byte, overrideKey string,
	onProgress func(uploaded, total int64)) error {

	// 先写 Fields（七牛要求 token 和 key 在 file 之前）
	for k, v := range d.Fields {
		if err := mw.WriteField(k, v); err != nil {
			return fmt.Errorf("写入字段 %s 失败: %w", k, err)
		}
	}
	// 批量模式：overrideKey 覆盖 key 字段（Fields 里没有 key，这里补上）
	if overrideKey != "" {
		if err := mw.WriteField("key", overrideKey); err != nil {
			return fmt.Errorf("写入 key 字段失败: %w", err)
		}
	}

	// 写文件 part（字段名 = fileField）
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fileField, fileName)}
	header["Content-Type"] = []string{"application/octet-stream"}
	part, err := mw.CreatePart(header)
	if err != nil {
		return fmt.Errorf("创建 file part 失败: %w", err)
	}

	// 文件内容经 progressReader 包装：HTTP client 从 pipe 读走多少字节，
	// 这里 io.Copy 才能往下写多少字节，Read 被调用 = 网络真实传输出进度。
	pr := &progressReader{
		r:          bytes.NewReader(data),
		uploaded:   0,
		total:      int64(len(data)),
		onProgress: onProgress,
	}
	if _, err := io.Copy(part, pr); err != nil {
		return fmt.Errorf("写入文件内容失败: %w", err)
	}

	if err := mw.Close(); err != nil {
		return fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}
	return nil
}
