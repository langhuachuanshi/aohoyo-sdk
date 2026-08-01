package s2s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestUploadBatch_Success 并发上传多个文件，全部成功
func TestUploadBatch_Success(t *testing.T) {
	var received int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/storage/upload" {
			t.Errorf("路径错误: %s", r.URL.Path)
		}
		// 简单校验签名头存在
		if r.Header.Get("X-App-ID") == "" || r.Header.Get("X-S2S-Signature") == "" {
			t.Error("缺少 S2S 签名头")
		}
		// 解析 multipart，确认有 file
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("解析 multipart 失败: %v", err)
		}
		if r.MultipartForm == nil || len(r.MultipartForm.File) == 0 {
			t.Error("multipart 无文件")
		}
		atomic.AddInt32(&received, 1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"url":       fmt.Sprintf("https://cdn.test.com/%s", r.FormValue("path")),
				"file_name": "x",
				"file_size": 100,
			},
		})
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	c.MaxConcurrency = 3

	items := make([]UploadItem, 10)
	for i := range items {
		items[i] = UploadItem{
			PathPrefix: fmt.Sprintf("docs/p%d/", i),
			FileName:   fmt.Sprintf("page%d.html", i),
			Data:       []byte("<html>test</html>"),
		}
	}

	var progressCalls int32
	res, err := c.UploadBatch(context.Background(), items, func(done, total int) {
		atomic.AddInt32(&progressCalls, 1)
		if total != 10 {
			t.Errorf("total 应为 10，实际 %d", total)
		}
		if done < 1 || done > 10 {
			t.Errorf("done 应在 1-10，实际 %d", done)
		}
	})
	if err != nil {
		t.Fatalf("UploadBatch 失败: %v", err)
	}

	if res.Success != 10 {
		t.Errorf("Success 应为 10，实际 %d", res.Success)
	}
	if res.Failed != 0 {
		t.Errorf("Failed 应为 0，实际 %d", res.Failed)
	}
	if int(received) != 10 {
		t.Errorf("服务器应收到 10 个请求，实际 %d", received)
	}
	if int(progressCalls) != 10 {
		t.Errorf("进度回调应调 10 次，实际 %d", progressCalls)
	}
	// 每个 URL 应非空
	for i, u := range res.URLs {
		if u == "" {
			t.Errorf("URLs[%d] 为空", i)
		}
	}
}

// TestUploadBatch_PartialFailure 部分失败：服务器对带 "fail" 的文件名返回 500
func TestUploadBatch_PartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(32 << 20)
		// 取真实文件名（multipart header 里的 filename）
		var fname string
		for _, files := range r.MultipartForm.File {
			if len(files) > 0 {
				fname = files[0].Filename
			}
			break
		}
		// 文件名含 "fail" 的返回 500
		if len(fname) >= 4 && fname[:4] == "fail" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"url": "https://cdn.test.com/ok", "file_name": fname, "file_size": 10},
		})
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	items := []UploadItem{
		{FileName: "fail1.html", Data: []byte("x")}, // 失败
		{FileName: "ok1.html", Data: []byte("x")},   // 成功
		{FileName: "fail2.html", Data: []byte("x")}, // 失败
		{FileName: "ok2.html", Data: []byte("x")},   // 成功
	}

	res, _ := c.UploadBatch(context.Background(), items, nil)

	if res.Success != 2 {
		t.Errorf("Success 应为 2，实际 %d", res.Success)
	}
	if res.Failed != 2 {
		t.Errorf("Failed 应为 2，实际 %d", res.Failed)
	}
	// 失败项有错误，成功项错误为 nil
	if res.Errors[0] == nil || res.Errors[2] == nil {
		t.Error("失败项应有错误记录")
	}
	if res.Errors[1] != nil || res.Errors[3] != nil {
		t.Error("成功项错误应为 nil")
	}
}

// TestUploadBatch_Empty 空列表
func TestUploadBatch_Empty(t *testing.T) {
	c := New("APP_test", "secret", "http://localhost")
	res, err := c.UploadBatch(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("空列表不应报错: %v", err)
	}
	if res.Success != 0 || res.Failed != 0 {
		t.Error("空列表结果应为 0")
	}
}

// TestUploadBatch_WithItemCallback per-item 回调：每个文件完成后立即回调，携带文件名、成功/失败、错误
func TestUploadBatch_WithItemCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(32 << 20)
		var fname string
		for _, files := range r.MultipartForm.File {
			if len(files) > 0 {
				fname = files[0].Filename
			}
			break
		}
		if fname == "fail.html" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"url": "https://cdn.test.com/" + fname, "file_name": fname, "file_size": 10},
		})
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	items := []UploadItem{
		{FileName: "ok1.html", Data: []byte("a")},
		{FileName: "fail.html", Data: []byte("b")},
		{FileName: "ok2.html", Data: []byte("c")},
	}

	// callbacks 在 worker goroutine（回调）与主 goroutine（断言）间共享，
	// 必须加锁，否则 -race 报数据竞争（append 扩容时的读写冲突）。
	var mu sync.Mutex
	var callbacks []ItemResult
	res, err := c.UploadBatch(context.Background(), items, nil, WithItemCallback(func(ir ItemResult) {
		mu.Lock()
		defer mu.Unlock()
		callbacks = append(callbacks, ir)
	}))
	if err != nil {
		t.Fatalf("UploadBatch 失败: %v", err)
	}

	// UploadBatch 返回后所有 worker 已 join，但保险起见仍加锁读取。
	mu.Lock()
	got := make([]ItemResult, len(callbacks))
	copy(got, callbacks)
	mu.Unlock()

	// 回调次数 = 文件数
	if len(got) != 3 {
		t.Fatalf("回调次数应为 3，实际 %d", len(got))
	}

	// 回调 Index 覆盖 0..2，文件名正确
	seen := make(map[int]ItemResult)
	for _, cb := range got {
		seen[cb.Index] = cb
		if cb.Index < 0 || cb.Index >= 3 {
			t.Errorf("Index 越界: %d", cb.Index)
		}
	}
	// ok1: 成功
	if seen[0].Err != nil {
		t.Errorf("ok1 应成功，实际 Err=%v", seen[0].Err)
	}
	if seen[0].URL == "" {
		t.Error("ok1 URL 不应为空")
	}
	if seen[0].Item.FileName != "ok1.html" {
		t.Errorf("ok1 文件名应为 ok1.html，实际 %s", seen[0].Item.FileName)
	}
	// fail: 失败
	if seen[1].Err == nil {
		t.Error("fail 应有错误")
	}
	if seen[1].URL != "" {
		t.Errorf("fail URL 应为空，实际 %s", seen[1].URL)
	}
	// ok2: 成功
	if seen[2].Err != nil {
		t.Errorf("ok2 应成功，实际 Err=%v", seen[2].Err)
	}

	// 与 BatchResult 一致
	if res.Success != 2 || res.Failed != 1 {
		t.Errorf("BatchResult 应为 Success=2,Failed=1，实际 %d,%d", res.Success, res.Failed)
	}
}

// TestUploadBatch_ContextCancel 超时取消：未派发的不再执行
func TestUploadBatch_ContextCancel(t *testing.T) {
	var received int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&received, 1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "message": "success",
			"data": map[string]any{"url": "https://x", "file_name": "x", "file_size": 1},
		})
	}))
	defer srv.Close()

	c := New("APP_test", "secret", srv.URL)
	c.MaxConcurrency = 1 // 单 worker，便于测试取消

	// 用已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	items := make([]UploadItem, 20)
	for i := range items {
		items[i] = UploadItem{FileName: "x.html", Data: []byte("x")}
	}

	res, _ := c.UploadBatch(ctx, items, nil)

	// 已取消的 context，所有任务应失败
	if res.Success != 0 {
		t.Errorf("取消后不应有成功，实际 %d", res.Success)
	}
	if res.Failed != 20 {
		t.Errorf("Failed 应为 20，实际 %d", res.Failed)
	}
}
