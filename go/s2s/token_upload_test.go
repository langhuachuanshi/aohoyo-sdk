package s2s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// mockQiniuEnv 模拟 AS 凭证接口 + 七牛上传端点
// 返回（asServer, qiniuServer, asURL）
func mockQiniuEnv(t *testing.T, directMode bool) (string, string) {
	t.Helper()

	// 七牛上传端点（接收直传）
	var qiniuReceived int32
	qiniuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 解析 multipart，确认有 token + key + file
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("七牛端点解析 multipart 失败: %v", err)
		}
		if r.MultipartForm == nil {
			t.Error("七牛端点未收到 multipart 表单")
		}
		if r.MultipartForm.Value["token"] == nil || r.MultipartForm.Value["key"] == nil {
			t.Error("七牛端点缺少 token 或 key 字段")
		}
		if len(r.MultipartForm.File) == 0 {
			t.Error("七牛端点缺少文件")
		}
		atomic.AddInt32(&qiniuReceived, 1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"hash": "fakehash",
			"key":  r.MultipartForm.Value["key"][0],
		})
	}))

	// AS 凭证接口
	asSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/storage/upload-token" {
			// 读 body 拿 filename + 可选 key
			raw, _ := io.ReadAll(r.Body)
			var req struct {
				Path     string `json:"path"`
				Filename string `json:"filename"`
				Key      string `json:"key"`
			}
			json.Unmarshal(raw, &req)

			if directMode {
				// 返回七牛直传指令
				// 调用方传了 key 就原样用（模拟 AS 行为），否则按默认重命名
				key := req.Key
				if key == "" {
					key = fmt.Sprintf("%s%s/file.html", req.Path, "20260107")
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"message": "success",
					"data": map[string]any{
						"mode":     "direct",
						"provider": "qiniu",
						"key":      key,
						"url":      "https://cdn.test.com/" + key,
						"upload": map[string]any{
							"method":     "POST",
							"endpoint":   qiniuSrv.URL,
							"file_field": "file",
							"fields": map[string]any{
								"token": "fake-uptoken-" + req.Filename,
								"key":   key,
							},
						},
					},
				})
			} else {
				// proxy 模式
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"code":    200,
					"message": "success",
					"data":    map[string]any{"mode": "proxy", "provider": "local"},
				})
			}
			return
		}

		// /storage/upload 代理上传（proxy 模式回退用）
		if r.URL.Path == "/storage/upload" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"code":    200,
				"message": "success",
				"data":    map[string]any{"url": "https://proxy.test.com/uploaded", "file_name": "x", "file_size": 10},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))

	t.Cleanup(func() {
		asSrv.Close()
		qiniuSrv.Close()
	})

	return asSrv.URL, qiniuSrv.URL
}

// TestUploadByToken_DirectQiniu 凭证直传七牛：文件不经 AS，直达七牛端点
func TestUploadByToken_DirectQiniu(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)
	res, err := c.UploadByToken(context.Background(), "docs/", "test.html", "", []byte("<html>"))
	if err != nil {
		t.Fatalf("UploadByToken 失败: %v", err)
	}

	if res.URL == "" {
		t.Error("URL 不应为空")
	}
	if res.FileName != "test.html" {
		t.Errorf("FileName 应为 test.html，实际 %s", res.FileName)
	}
}

// TestUploadByToken_CustomKey 传完整 key 保持目录结构（CHM 站点场景）
func TestUploadByToken_CustomKey(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)
	// CHM 站点：传完整 key，保持目录结构
	res, err := c.UploadByToken(context.Background(), "docs/教程/", "index.html",
		"docs/教程/index.html", []byte("<html>"))
	if err != nil {
		t.Fatalf("UploadByToken 失败: %v", err)
	}
	// URL 应该包含完整的 key（不是日期/纳秒重命名）
	if res.URL != "https://cdn.test.com/docs/教程/index.html" {
		t.Errorf("URL 应保持原始 key 路径，实际: %s", res.URL)
	}
}

// TestUploadBatchByToken_CustomKey 批量传 Key 保持目录结构
func TestUploadBatchByToken_CustomKey(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)
	c.RatePerSec = 100

	items := []UploadItem{
		{PathPrefix: "docs/教程/", FileName: "index.html", Key: "docs/教程/index.html", Data: []byte("x")},
		{PathPrefix: "docs/教程/content/", FileName: "page.html", Key: "docs/教程/content/page.html", Data: []byte("x")},
	}

	res, err := c.UploadBatchByToken(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("UploadBatchByToken 失败: %v", err)
	}
	if res.Success != 2 {
		t.Fatalf("Success 应为 2，实际 %d", res.Success)
	}
	// 验证 URL 保持原始 key（CHM 相对链接不 404）
	expectedURLs := map[string]bool{
		"https://cdn.test.com/docs/教程/index.html":        true,
		"https://cdn.test.com/docs/教程/content/page.html": true,
	}
	for _, u := range res.URLs {
		if !expectedURLs[u] {
			t.Errorf("意外的 URL: %s（应保持原始 key 路径）", u)
		}
	}
}
func TestUploadByToken_ProxyFallback(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, false)

	c := New("APP_test", "secret", asURL)
	res, err := c.UploadByToken(context.Background(), "docs/", "test.html", "", []byte("<html>"))
	if err != nil {
		t.Fatalf("UploadByToken 失败: %v", err)
	}

	// proxy 模式 URL 来自代理接口的固定值
	if res.URL != "https://proxy.test.com/uploaded" {
		t.Errorf("proxy 回退 URL 错误: %s", res.URL)
	}
}

// TestUploadBatchByToken_Direct 批量凭证直传
func TestUploadBatchByToken_Direct(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, true)

	c := New("APP_test", "secret", asURL)
	c.MaxConcurrency = 3
	c.RatePerSec = 100 // 测试不节流

	items := make([]UploadItem, 10)
	for i := range items {
		items[i] = UploadItem{
			PathPrefix: "docs/",
			FileName:   fmt.Sprintf("page%d.html", i),
			Data:       []byte("<html>test</html>"),
		}
	}

	var progressCalls int32
	res, err := c.UploadBatchByToken(context.Background(), items, func(done, total int) {
		atomic.AddInt32(&progressCalls, 1)
	})
	if err != nil {
		t.Fatalf("UploadBatchByToken 失败: %v", err)
	}

	if res.Success != 10 {
		t.Errorf("Success 应为 10，实际 %d", res.Success)
	}
	if res.Failed != 0 {
		t.Errorf("Failed 应为 0，实际 %d", res.Failed)
	}
	if int(progressCalls) != 10 {
		t.Errorf("进度回调应调 10 次，实际 %d", progressCalls)
	}
}

// TestUploadBatchByToken_ProxyFallback 批量 proxy 模式全部回退代理
func TestUploadBatchByToken_ProxyFallback(t *testing.T) {
	asURL, _ := mockQiniuEnv(t, false)

	c := New("APP_test", "secret", asURL)
	c.RatePerSec = 100

	items := []UploadItem{
		{FileName: "a.html", Data: []byte("x")},
		{FileName: "b.html", Data: []byte("x")},
	}

	res, err := c.UploadBatchByToken(context.Background(), items, nil)
	if err != nil {
		t.Fatalf("UploadBatchByToken 失败: %v", err)
	}

	if res.Success != 2 {
		t.Errorf("Success 应为 2，实际 %d", res.Success)
	}
}
