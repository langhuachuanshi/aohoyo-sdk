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
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// UploadItem 批量上传的单个文件项
type UploadItem struct {
	PathPrefix string // 存储目录前缀，如 "docs/教程/content/"（空则服务端默认 "uploads/"）
	FileName   string // 文件名，如 "page.html"
	Data       []byte // 文件内容字节
	Key        string // 可选：完整对象 key（如 "docs/教程/index.html"），传了 AS 按此 key 签凭证；
	//      不传则 AS 按 PathPrefix+日期/纳秒自动生成（防重名）。CHM 站点传 Key 保持目录结构。
}

// BatchResult 批量上传结果
type BatchResult struct {
	URLs    []string // 每个文件的访问 URL（顺序与 items 对应，失败的为空字符串）
	Success int      // 成功数
	Failed  int      // 失败数
	Errors  []error  // 失败项的错误（顺序对应 items，成功的为 nil）
}

// ItemResult 单文件上传结果（per-item 回调参数）
type ItemResult struct {
	Index int        // 在 items 切片中的下标（从 0 开始）
	Item  UploadItem // 原始上传项
	URL   string     // 上传成功时的访问 URL，失败时为空
	Err   error      // 上传失败时的错误，成功时为 nil
}

// BatchOption 批量上传的可选配置（函数式选项模式，向后兼容）
type BatchOption func(*batchConfig)

type batchConfig struct {
	onItem func(ItemResult) // per-item 回调，worker 完成每个文件后立即调用（并发安全）
}

// WithItemCallback 设置 per-item 回调：worker 每完成一个文件（无论成功失败）立即调用。
// 回调在 worker goroutine 中执行，需并发安全。可为 nil（等同于不设置）。
//
// 适用场景：实时滚动日志面板、边传边显示每个文件状态。
func WithItemCallback(fn func(ItemResult)) BatchOption {
	return func(cfg *batchConfig) {
		cfg.onItem = fn
	}
}

// UploadBatch 并发批量上传文件。
//
// 内部用 worker 池并发（并发数由 c.MaxConcurrency 控制，默认 5），
// 单文件失败不中断整体，全部完成后返回聚合结果。
//
// onProgress：每完成一个文件调一次（无论成功失败），传 (done, total)，可为 nil。
// 调用方可在回调里更新进度条。并发安全（done 用 atomic 计数）。
//
// ctx 取消：停止派发新任务，已在飞的请求会完成。返回时 done 可能小于 total。
//
// 适用场景：CHM 转站点等需要一次上传数百个文件的场景。
// S2S 签名仍按文件独立计算（body 不同无法复用），并发只能省 HTTP 等待时间。
func (c *Client) UploadBatch(ctx context.Context, items []UploadItem, onProgress func(done, total int), opts ...BatchOption) (*BatchResult, error) {
	return c.uploadBatchGeneric(ctx, items, onProgress, c.uploadSingle, opts...)
}

// uploadBatchGeneric 通用批量上传引擎（worker 池 + 节流 + 进度）。
//
// singleFn 决定每个文件怎么传：
//   - uploadSingle（代理上传）：UploadBatch 用
//   - uploadSingleByToken（凭证直传）：UploadBatchByToken 用
//
// 这样节流/并发/重试/进度逻辑只写一份，两种上传模式共用。
func (c *Client) uploadBatchGeneric(
	ctx context.Context,
	items []UploadItem,
	onProgress func(done, total int),
	singleFn func(ctx context.Context, item UploadItem) (string, error),
	opts ...BatchOption,
) (*BatchResult, error) {
	// 解析可选配置
	var cfg batchConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	total := len(items)
	if total == 0 {
		return &BatchResult{URLs: []string{}, Errors: []error{}}, nil
	}

	concurrency := c.MaxConcurrency
	if concurrency <= 0 {
		concurrency = defaultMaxConcurrency
	}
	if concurrency > total {
		concurrency = total
	}

	result := &BatchResult{
		URLs:   make([]string, total),
		Errors: make([]error, total),
	}
	// 并发计数器（worker 之间汇总 Success/Failed 用，避免数据竞争）
	var success, failed, done int64

	// 客户端节流：令牌桶，每秒填充 RatePerSec 个令牌。
	ratePerSec := c.RatePerSec
	if ratePerSec <= 0 {
		ratePerSec = defaultRatePerSec
	}
	throttle := time.NewTicker(time.Second / time.Duration(ratePerSec))
	defer throttle.Stop()

	// 任务队列：每个任务带原始 index，保证结果顺序与 items 对应
	type job struct {
		index int
		item  UploadItem
	}
	jobCh := make(chan job, total)
	for i, it := range items {
		jobCh <- job{index: i, item: it}
	}
	close(jobCh)

	// worker 池
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				// ctx 取消：停止处理剩余任务（已派发的等其完成或被 HTTP 层取消）
				if err := ctx.Err(); err != nil {
					cancelErr := fmt.Errorf("已取消: %w", err)
					result.Errors[j.index] = cancelErr
					atomic.AddInt64(&failed, 1)
					d := atomic.AddInt64(&done, 1)
					if onProgress != nil {
						onProgress(int(d), total)
					}
					if cfg.onItem != nil {
						cfg.onItem(ItemResult{Index: j.index, Item: j.item, Err: cancelErr})
					}
					continue
				}

				// 节流：等待令牌（限速到 RatePerSec 以内，避免打爆 AS）
				select {
				case <-throttle.C:
				case <-ctx.Done():
					cancelErr := fmt.Errorf("已取消: %w", ctx.Err())
					result.Errors[j.index] = cancelErr
					atomic.AddInt64(&failed, 1)
					d := atomic.AddInt64(&done, 1)
					if onProgress != nil {
						onProgress(int(d), total)
					}
					if cfg.onItem != nil {
						cfg.onItem(ItemResult{Index: j.index, Item: j.item, Err: cancelErr})
					}
					continue
				}

				url, err := singleFn(ctx, j.item)
				if err != nil {
					result.Errors[j.index] = err
					atomic.AddInt64(&failed, 1)
				} else {
					result.URLs[j.index] = url
					atomic.AddInt64(&success, 1)
				}
				d := atomic.AddInt64(&done, 1)
				if onProgress != nil {
					onProgress(int(d), total)
				}
				if cfg.onItem != nil {
					cfg.onItem(ItemResult{Index: j.index, Item: j.item, URL: url, Err: err})
				}
			}
		}()
	}
	wg.Wait()

	result.Success = int(success)
	result.Failed = int(failed)
	return result, nil
}

// uploadSingle 上传单个文件，返回 URL。遇到 429 自动退避重试。
// 从 Upload 方法抽取的核心逻辑，供批量上传复用（不暴露给外部）。
func (c *Client) uploadSingle(ctx context.Context, item UploadItem) (string, error) {
	if int64(len(item.Data)) > c.MaxUploadBytes {
		return "", fmt.Errorf("文件 %s 大小 %d 超过上限 %d", item.FileName, len(item.Data), c.MaxUploadBytes)
	}

	// 按文件大小动态算超时（v0.9.0），包在整个重试循环外：
	// 批量上传单个大文件不再被固定 30s 切断；CHM 整站等数百 MB 场景也能传完。
	ctx, cancel := withUploadTimeout(ctx, int64(len(item.Data)))
	defer cancel()

	// 构造 multipart body（body 内容稳定，可复用；但签名每次不同，需重新算）
	bodyBytes, contentType, err := buildMultipartBody(item)
	if err != nil {
		return "", err
	}

	maxRetries := c.MaxRetries
	if maxRetries < 0 {
		maxRetries = defaultMaxRetries
	}

	var lastErr error
	var retryAfter time.Duration // 上次 429 的 Retry-After（局部变量，每次循环更新）
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 非首次尝试：退避等待（指数退避 1s/2s/4s，或读上次的 Retry-After）
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			if retryAfter > 0 && retryAfter < 60*time.Second {
				backoff = retryAfter
			}
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", fmt.Errorf("上传 %s 已取消: %w", item.FileName, ctx.Err())
			}
		}

		// 每次重新签名（timestamp 变了，签名必须重算）
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.BaseURL+"/as/v1/storage/upload", bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", contentType)
		for k, v := range SignHeaders(c.AppID, c.AppSecret, bodyBytes) {
			req.Header.Set(k, v)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("上传 %s 失败: %w", item.FileName, err)
			continue // 网络错误也重试
		}

		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("读取 %s 响应失败: %w", item.FileName, readErr)
			continue
		}

		// 429 限流：读 Retry-After，退避后重试
		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			lastErr = fmt.Errorf("上传 %s 被限流 (HTTP 429)，第 %d 次重试", item.FileName, attempt+1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("上传 %s 失败 (HTTP %d): %s", item.FileName, resp.StatusCode, string(raw))
		}

		var r apiResp
		if err := json.Unmarshal(raw, &r); err != nil {
			return "", fmt.Errorf("解析 %s 响应失败: %w", item.FileName, err)
		}
		if r.Code != 200 {
			return "", fmt.Errorf("上传 %s 业务错误: %s", item.FileName, r.Msg)
		}

		var out UploadResult
		if err := json.Unmarshal(r.Data, &out); err != nil {
			return "", fmt.Errorf("解析 %s 结果失败: %w", item.FileName, err)
		}
		return out.URL, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("上传 %s 失败（已重试 %d 次）: %w", item.FileName, maxRetries, lastErr)
	}
	return "", fmt.Errorf("上传 %s 失败（未知原因）", item.FileName)
}

// buildMultipartBody 构造 multipart 请求体，返回 body 字节 + Content-Type
func buildMultipartBody(item UploadItem) ([]byte, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if item.PathPrefix != "" {
		if err := writer.WriteField("path", item.PathPrefix); err != nil {
			return nil, "", fmt.Errorf("写入 path 失败: %w", err)
		}
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, item.FileName))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("创建 file part 失败: %w", err)
	}
	if _, err := part.Write(item.Data); err != nil {
		return nil, "", fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	return body.Bytes(), writer.FormDataContentType(), nil
}

// parseRetryAfter 解析 Retry-After 头（秒数），失败返回 0
func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 0
	}
	secs, err := strconv.Atoi(s)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
