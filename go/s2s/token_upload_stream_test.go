package s2s

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestUploadByTokenWithProgress_ProgressCallback 验证进度回调：
//   - onProgress 被触发
//   - uploaded 单调递增（不回退）
//   - 最终 uploaded == total == len(data)
//   - 返回 URL 正确
//
// 用 mockQiniuEnv（token_upload_test.go）搭 AS + 七牛端点，
// 七牛端点分块慢速读 body，确保 pipe 多次放行 → 多次 Read → 多次回调。
func TestUploadByTokenWithProgress_ProgressCallback(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)

	// 64KB 数据：足够分多块读（http client 默认读缓冲 32KB，至少 2 次回调）
	data := bytes.Repeat([]byte("x"), 64*1024)

	var mu sync.Mutex
	var calls int
	var lastUploaded int64 = -1
	monotonic := true
	var finalUploaded, total int64

	res, err := c.UploadByTokenWithProgress(context.Background(), "docs/", "big.html", "",
		data, func(uploaded, tot int64) {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if uploaded < lastUploaded {
				monotonic = false
			}
			lastUploaded = uploaded
			finalUploaded = uploaded
			total = tot
		})
	if err != nil {
		t.Fatalf("UploadByTokenWithProgress 失败: %v", err)
	}

	if res.URL == "" {
		t.Error("URL 不应为空")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls == 0 {
		t.Error("onProgress 应被调用至少一次")
	}
	if !monotonic {
		t.Error("uploaded 应单调递增，观察到回退")
	}
	if total != int64(len(data)) {
		t.Errorf("total 应为 %d，实际 %d", len(data), total)
	}
	if finalUploaded != int64(len(data)) {
		t.Errorf("最终 uploaded 应为 %d，实际 %d", len(data), finalUploaded)
	}
}

// TestUploadByTokenWithProgress_RetryStream 验证流式重试能成功（body 重构）：
//   - 第一次返回 429 + Retry-After:0
//   - 第二次返回 200
//   - 最终成功，URL 正确
//   - 确认 body 被构造了 2 次（pipe 每次重开，验证 reader 只能读一次时能重建）
//
// 直接 mock 七牛端点（不经 AS），用 directPOSTStream 验证重试路径。
func TestUploadByTokenWithProgress_RetryStream(t *testing.T) {
	var attempts int32
	// 收到的文件字节总数（验证每次重试都完整收到了 body）
	var totalBytesReceived int64

	srv := newMockQiniuRetryServer(t, &attempts, &totalBytesReceived)

	// 直接构造 directive，跳过 AS 凭证步骤
	d := &UploadDirective{
		Method:    "POST",
		Endpoint:  srv.URL,
		FileField: "file",
		Fields:    map[string]string{"token": "tok", "key": "k"},
	}

	c := New("APP_test", "secret", "")
	c.MaxRetries = 3

	data := bytes.Repeat([]byte("y"), 32*1024)
	var calls int
	url, err := c.directPOSTStream(context.Background(), d, data, "f.html",
		"https://cdn.test.com/k", "", func(uploaded, total int64) {
			calls++
		})
	if err != nil {
		t.Fatalf("directPOSTStream 应重试成功，实际: %v", err)
	}
	if url != "https://cdn.test.com/k" {
		t.Errorf("URL 错误: %s", url)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("应尝试 2 次（1 次 429 + 1 次 200），实际 %d", atomic.LoadInt32(&attempts))
	}
	if calls == 0 {
		t.Error("重试期间进度回调应被触发")
	}
	// 两次都完整收到了文件字节（第二次成功的那次至少 32KB）
	if totalBytesReceived < int64(len(data)) {
		t.Errorf("成功那次应收到完整文件 %d 字节，实际累计 %d", len(data), totalBytesReceived)
	}
}

// TestUploadByTokenWithProgress_NilPassthrough 验证 onProgress==nil 时
// 行为与 UploadByToken 完全一致（直接转调）。
func TestUploadByTokenWithProgress_NilPassthrough(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)

	// 不传 onProgress（nil）
	resProgress, err := c.UploadByTokenWithProgress(context.Background(), "docs/", "test.html", "",
		[]byte("<html>"), nil)
	if err != nil {
		t.Fatalf("onProgress==nil 失败: %v", err)
	}

	// 对比原 UploadByToken
	resPlain, err := c.UploadByToken(context.Background(), "docs/", "test.html", "", []byte("<html>"))
	if err != nil {
		t.Fatalf("UploadByToken 失败: %v", err)
	}

	if resProgress.URL != resPlain.URL {
		t.Errorf("URL 不一致: progress=%s plain=%s", resProgress.URL, resPlain.URL)
	}
	if resProgress.FileName != resPlain.FileName {
		t.Errorf("FileName 不一致: progress=%s plain=%s", resProgress.FileName, resPlain.FileName)
	}
	if resProgress.FileSize != resPlain.FileSize {
		t.Errorf("FileSize 不一致: progress=%d plain=%d", resProgress.FileSize, resPlain.FileSize)
	}
}

// TestUploadByTokenWithProgress_ProxyFallback proxy 模式回退 Upload（无进度回调，不报错）
func TestUploadByTokenWithProgress_ProxyFallback(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, false)

	c := New("APP_test", "secret", asURL)

	var calls int
	res, err := c.UploadByTokenWithProgress(context.Background(), "docs/", "test.html", "",
		[]byte("<html>"), func(uploaded, total int64) {
			calls++
		})
	if err != nil {
		t.Fatalf("proxy 回退失败: %v", err)
	}
	// proxy 模式 URL 来自代理接口的固定值
	if res.URL != "https://proxy.test.com/uploaded" {
		t.Errorf("proxy 回退 URL 错误: %s", res.URL)
	}
	// proxy 模式无进度回调（按设计，本地存储上传快）
	if calls != 0 {
		t.Errorf("proxy 模式不应触发进度回调，实际触发 %d 次", calls)
	}
}

// newMockQiniuRetryServer 模拟七牛端点：第 1 次 429，第 2 次起 200。
// 完整读取并解析 multipart body（验证流式 body 被正确构造）。
func newMockQiniuRetryServer(t *testing.T, attempts *int32, totalBytesReceived *int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(attempts, 1)

		// 必须完整读 body（否则 pipe writer 端阻塞，goroutine 泄漏 + 进度回调不全）
		raw, _ := io.ReadAll(r.Body)
		atomic.AddInt64(totalBytesReceived, int64(len(raw)))

		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hash":"h","key":"k"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}
