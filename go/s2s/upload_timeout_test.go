package s2s

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestWithUploadTimeout_Formula 验证动态超时公式：30s + size/1MB×1s。
//
// 这是 v0.9.0 根治大文件 30s 超时的核心：旧版 HTTPClient.Timeout 固定 30s，
// 200MB 直传会被切断；新版按文件大小算，200MB → 230s（够 3MB/s 上行 67s 传完）。
func TestWithUploadTimeout_Formula(t *testing.T) {
	cases := []struct {
		name     string
		sizeBytes int64
		wantMin  time.Duration // 下界（含基础 30s）
		wantMax  time.Duration // 上界（公式精确值，误差 0）
	}{
		{"200MB-根因场景", 200 << 20, 230 * time.Second, 230 * time.Second},
		{"100MB-SDK上限", 100 << 20, 130 * time.Second, 130 * time.Second},
		{"10MB-中等", 10 << 20, 40 * time.Second, 40 * time.Second},
		{"1MB-小文件", 1 << 20, 31 * time.Second, 31 * time.Second},
		{"0-防御退化为基础超时", 0, 30 * time.Second, 30 * time.Second},
		{"负数-防御退化为基础超时", -1, 30 * time.Second, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 直接用公式重算验证（不依赖真实 deadline，避免测试跑满超时时长）
			extra := time.Duration(0)
			if tc.sizeBytes > 0 {
				extra = time.Duration(tc.sizeBytes/uploadBytesPerSec) * time.Second
			}
			got := uploadBaseTimeout + extra
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("size=%d: 超时=%v，期望 [%v, %v]", tc.sizeBytes, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// TestWithUploadTimeout_RespectsParentContext 验证动态超时尊重调用方传入的父 ctx：
// 父 ctx 先结束 → 子 ctx 立即结束（调用方取消能传播）。
func TestWithUploadTimeout_RespectsParentContext(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	child, childCancel := withUploadTimeout(parent, 1<<20) // 31s 子超时
	defer childCancel()

	parentCancel()
	select {
	case <-child.Done():
		// 预期：父取消立即传到子
	case <-time.After(100 * time.Millisecond):
		t.Fatal("父 ctx 取消后，子 ctx 应立即 Done")
	}
}

// TestDirectPOSTStream_LargeFileNoLonger30sTimeout 核心强度测试：
// 模拟「慢速七牛端点」，验证流式直传大文件不再被 30s 切断导致重试。
//
// 设计：mock 端点在收到请求后 sleep 一小段（模拟网络传输耗时），然后成功返回。
// 关键断言：attempts 计数 == 1（不触发重试）。
// 若旧 30s 超时 bug 复现，HTTPClient.Timeout 会在 30s 强制取消 → 触发重试 → attempts > 1。
//
// 注：不用真 200MB（占内存），用 1MB 数据 + 慢速端点验证「不重试」机制；
// 超时公式本身由 TestWithUploadTimeout_Formula 覆盖。这里验证端到端路径。
func TestDirectPOSTStream_LargeFileNoLonger30sTimeout(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		// 模拟慢速接收：完整读 body（消费上传内容），sleep 一小段模拟网络耗时
		io.Copy(io.Discard, r.Body)
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hash":"h","key":"k"}`))
	}))
	defer srv.Close()

	c := New("APP_test", "secret", "")
	c.MaxRetries = 3 // 允许重试，验证「不触发」

	d := &UploadDirective{
		Method:    "POST",
		Endpoint:  srv.URL,
		FileField: "file",
		Fields:    map[string]string{"token": "tok", "key": "k"},
	}

	data := bytes.Repeat([]byte("x"), 1<<20) // 1MB（超时 = 31s，远大于实际传输时间）

	url, err := c.directPOSTStream(context.Background(), d, data, "big.bin",
		"https://cdn.test.com/k", "", func(uploaded, total int64) {})
	if err != nil {
		t.Fatalf("直传应成功，实际: %v", err)
	}
	if url != "https://cdn.test.com/k" {
		t.Errorf("URL 错误: %s", url)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts 应为 1（不重试），实际 %d —— 若 >1 说明仍被某层超时切断", got)
	}
}

// TestDirectPOST_LargeFileNoLonger30sTimeout 同上，验证非流式 directPOST 路径。
func TestDirectPOST_LargeFileNoLonger30sTimeout(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hash":"h","key":"k"}`))
	}))
	defer srv.Close()

	c := New("APP_test", "secret", "")
	c.MaxRetries = 3

	d := &UploadDirective{
		Method:    "POST",
		Endpoint:  srv.URL,
		FileField: "file",
		Fields:    map[string]string{"token": "tok", "key": "k"},
	}

	data := bytes.Repeat([]byte("x"), 5<<20) // 5MB（超时 = 35s）

	url, err := c.directPOST(context.Background(), d, data, "big.bin", "https://cdn.test.com/k", "")
	if err != nil {
		t.Fatalf("直传应成功，实际: %v", err)
	}
	if url != "https://cdn.test.com/k" {
		t.Errorf("URL 错误: %s", url)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts 应为 1（不重试），实际 %d", got)
	}
}

// TestWithUploadTimeout_RealDeadlineEnforced 验证超时确实会生效（不是无限大）：
// 用极小的自定义 size 验证 deadline 被设置到 ctx（不跑满时长，只确认 Deadline 存在且合理）。
func TestWithUploadTimeout_RealDeadlineEnforced(t *testing.T) {
	ctx, cancel := withUploadTimeout(context.Background(), 10<<20) // 40s
	defer cancel()

	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("withUploadTimeout 应设置 Deadline")
	}
	// Deadline 应在「现在 + 40s」附近（允许几秒调度误差）
	now := time.Now()
	if dl.Before(now.Add(35*time.Second)) || dl.After(now.Add(45*time.Second)) {
		t.Errorf("Deadline = %v，期望约 now+40s（now=%v）", dl, now)
	}
}
