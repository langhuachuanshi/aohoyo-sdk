package s2s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestUploadBatch_RetryOn429 验证 429 自动重试：服务器前 2 次返回 429，第 3 次成功
func TestUploadBatch_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			// 前 2 次返回 429 + Retry-After: 0（测试用极短退避）
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// 第 3 次成功
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"url": "https://cdn.test.com/ok", "file_name": "x", "file_size": 1},
		})
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	c.MaxRetries = 3
	c.RatePerSec = 100 // 测试时不节流，专注验证重试

	items := []UploadItem{
		{FileName: "test.html", Data: []byte("x")},
	}
	res, err := c.UploadBatch(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("UploadBatch 失败: %v", err)
	}
	if res.Success != 1 {
		t.Errorf("应成功 1 个，实际 %d（attempts=%d）", res.Success, attempts)
	}
	if int(attempts) < 3 {
		t.Errorf("应至少重试到第 3 次，实际 %d", attempts)
	}
}

// TestUploadBatch_RetryExhausted 验证重试耗尽后正确标记失败
func TestUploadBatch_RetryExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	c.MaxRetries = 2 // 重试 2 次（共 3 次尝试）
	c.RatePerSec = 100

	items := []UploadItem{
		{FileName: "doomed.html", Data: []byte("x")},
	}
	res, _ := c.UploadBatch(context.Background(), items, nil)

	if res.Success != 0 {
		t.Errorf("应 0 成功，实际 %d", res.Success)
	}
	if res.Failed != 1 {
		t.Errorf("应 1 失败，实际 %d", res.Failed)
	}
	if res.Errors[0] == nil {
		t.Error("应有错误记录")
	}
}
