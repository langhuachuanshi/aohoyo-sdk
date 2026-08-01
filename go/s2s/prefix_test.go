package s2s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// mockPrefixEnv 模拟 AS 凭证接口（返回 bucket 级凭证）+ 七牛上传端点
// 验证：1 次拿凭证 + N 次直传，每文件 key 不同
func mockPrefixEnv(t *testing.T) (string, string) {
	t.Helper()

	var qiniuReceivedKeys []string // 收到的所有 key（不关心顺序，用计数验证）
	var qiniuReceived int32

	qiniuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(32 << 20)
		// 验证每次都带 token 和 key
		if r.MultipartForm.Value["token"] == nil {
			t.Error("七牛端点缺少 token")
		}
		keys := r.MultipartForm.Value["key"]
		if len(keys) == 0 {
			t.Error("七牛端点缺少 key")
		} else {
			qiniuReceivedKeys = append(qiniuReceivedKeys, keys[0])
		}
		if len(r.MultipartForm.File) == 0 {
			t.Error("七牛端点缺少文件")
		}
		atomic.AddInt32(&qiniuReceived, 1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"hash": "h", "key": keys[0]})
	}))

	var tokenRequested int32
	asSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/storage/upload-token" {
			atomic.AddInt32(&tokenRequested, 1)
			// 验证请求带 mode=prefix
			// 返回 bucket 级凭证（Fields 不含 key，KeyPrefix 填前缀）
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": map[string]any{
					"mode":     "direct",
					"provider": "qiniu",
					"url":      "https://cdn.test.com/docs/教程/",
					"upload": map[string]any{
						"method":     "POST",
						"endpoint":   qiniuSrv.URL,
						"file_field": "file",
						"fields":     map[string]any{"token": "bucket-level-token"},
						"key_prefix": "docs/教程/",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	t.Cleanup(func() {
		asSrv.Close()
		qiniuSrv.Close()
	})

	// 用闭包暴露计数器供测试断言
	t.Cleanup(func() {
		if int(atomic.LoadInt32(&tokenRequested)) != 1 {
			t.Errorf("凭证接口应只调 1 次，实际 %d 次", tokenRequested)
		}
		if len(qiniuReceivedKeys) > 0 {
			t.Logf("七牛收到 key: %v", qiniuReceivedKeys)
		}
	})

	return asSrv.URL, qiniuSrv.URL
}

// TestUploadBatchByPrefix_Success 1 次拿凭证 + 10 文件直传
func TestUploadBatchByPrefix_Success(t *testing.T) {
	asURL, _ := mockPrefixEnv(t)

	c := New("APP_test", "secret", asURL)
	c.MaxConcurrency = 5
	c.RatePerSec = 100 // 测试不节流

	items := make([]UploadItem, 10)
	for i := range items {
		items[i] = UploadItem{
			FileName: fmt.Sprintf("content/page%d.html", i), // 含子目录路径
			Data:     []byte("<html>"),
		}
	}

	res, err := c.UploadBatchByPrefix(context.Background(), "docs/教程/", items, nil)
	if err != nil {
		t.Fatalf("UploadBatchByPrefix 失败: %v", err)
	}

	if res.Success != 10 {
		t.Errorf("Success 应为 10，实际 %d", res.Success)
	}
	if res.Failed != 0 {
		t.Errorf("Failed 应为 0，实际 %d", res.Failed)
	}

	// 验证 URL 保持目录结构（KeyPrefix + FileName）
	for i, u := range res.URLs {
		expected := "https://cdn.test.com/docs/教程/content/page" + fmt.Sprint(i) + ".html"
		if u != expected {
			t.Errorf("URLs[%d] = %s，期望 %s", i, u, expected)
		}
	}
}

// TestUploadBatchByPrefix_CustomKey item.Key 非空时优先用
func TestUploadBatchByPrefix_CustomKey(t *testing.T) {
	asURL, _ := mockPrefixEnv(t)

	c := New("APP_test", "secret", asURL)
	c.RatePerSec = 100

	items := []UploadItem{
		{FileName: "index.html", Key: "docs/教程/index.html", Data: []byte("x")},
		{FileName: "style.css", Key: "docs/教程/assets/style.css", Data: []byte("x")},
	}

	res, err := c.UploadBatchByPrefix(context.Background(), "docs/教程/", items, nil)
	if err != nil {
		t.Fatalf("UploadBatchByPrefix 失败: %v", err)
	}
	if res.Success != 2 {
		t.Errorf("Success 应为 2，实际 %d", res.Success)
	}
}

// TestUploadBatchByPrefix_ProxyFallback 非七牛回退代理
func TestUploadBatchByPrefix_ProxyFallback(t *testing.T) {
	// mock AS 返回 proxy 模式
	asSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"mode": "proxy", "provider": "local"},
		})
	}))
	defer asSrv.Close()

	// mock 代理上传端点
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"url": "https://proxy.test.com/x", "file_name": "x", "file_size": 1},
		})
	}))
	defer proxySrv.Close()

	c := New("APP_test", "secret", asSrv.URL)
	c.RatePerSec = 100

	items := []UploadItem{
		{FileName: "a.html", Data: []byte("x")},
		{FileName: "b.html", Data: []byte("x")},
	}

	res, err := c.UploadBatchByPrefix(context.Background(), "docs/", items, nil)
	if err != nil {
		t.Fatalf("UploadBatchByPrefix 失败: %v", err)
	}

	if res.Success != 2 {
		t.Errorf("proxy 回退后 Success 应为 2，实际 %d", res.Success)
	}
}
