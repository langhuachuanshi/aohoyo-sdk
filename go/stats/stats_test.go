package stats

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestReportEvents_ServerReceivesCorrectPayload 用本地 mock server 验证：
// ① 请求路径正确 ② body 结构匹配服务端 StatsEventBatch ③ 默认值补全生效
func TestReportEvents_ServerReceivesCorrectPayload(t *testing.T) {
	var received struct {
		Events []Event `json:"events"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats/events" {
			t.Errorf("请求路径错误: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type 错误: %s", r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := New("APP_test", srv.URL)
	err := c.ReportEvents(context.Background(), []Event{
		{EventType: EventCustom, SessionID: "s1", Platform: PlatformWindows, Path: "/x"},
		{EventType: EventPageView, SessionID: "s2", Platform: PlatformWeb, Path: "/y"},
	})
	if err != nil {
		t.Fatalf("ReportEvents 失败: %v", err)
	}

	// 验证收到 2 条
	if len(received.Events) != 2 {
		t.Fatalf("应收到 2 条事件，实际 %d", len(received.Events))
	}

	// 验证 AppID 被补全（Event 里没填，应取 Client.AppID）
	for i, e := range received.Events {
		if e.AppID != "APP_test" {
			t.Errorf("事件 %d 的 AppID 未补全: %q", i, e.AppID)
		}
		if e.ClientTS == 0 {
			t.Errorf("事件 %d 的 ClientTS 未补全", i)
		}
	}
}

// TestReportEvents_EmptyAndOverflow 边界校验
func TestReportEvents_EmptyAndOverflow(t *testing.T) {
	c := New("APP_x", "http://localhost")

	if err := c.ReportEvents(context.Background(), nil); err == nil {
		t.Error("空列表应报错")
	}

	overflow := make([]Event, maxBatchSize+1)
	for i := range overflow {
		overflow[i] = Event{EventType: "custom", SessionID: "s", Platform: "web"}
	}
	if err := c.ReportEvents(context.Background(), overflow); err == nil {
		t.Error("超过上限应报错")
	}
}

// TestReportEvent_Single 单条便捷封装
func TestReportEvent_Single(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New("APP_test", srv.URL)
	if err := c.ReportEvent(context.Background(), Event{
		EventType: EventError, SessionID: "s1", Platform: PlatformWindows,
		ErrorMsg: "boom",
	}); err != nil {
		t.Fatalf("ReportEvent 失败: %v", err)
	}
}

// TestTrimSuffix 确认尾斜杠处理
func TestTrimSuffix(t *testing.T) {
	cases := map[string]string{
		"https://x.com/": "https://x.com",
		"https://x.com":  "https://x.com",
		"/":              "",
		"":               "",
	}
	for in, want := range cases {
		if got := trimSuffix(in, "/"); got != want {
			t.Errorf("trimSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
