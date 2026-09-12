package native

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetKV 服务端返回合并 KV 时正确解包；请求头带齐 DeviceSign 四件套
func TestGetKV(t *testing.T) {
	var gotHeaders map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/as/v1/app/kv" {
			t.Errorf("path = %s, want /as/v1/app/kv", r.URL.Path)
		}
		if q := r.URL.Query().Get("key"); q != "" {
			t.Errorf("no keys passed but got key query = %q", q)
		}
		gotHeaders = map[string]string{
			"X-App-ID":    r.Header.Get("X-App-ID"),
			"X-Device-ID": r.Header.Get("X-Device-ID"),
			"X-Sign":      r.Header.Get("X-Device-Sign"),
			"X-Timestamp": r.Header.Get("X-Timestamp"),
		}
		body, _ := json.Marshal(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"announcement": "hello",
				"max_count":    42,
				"enabled":      true,
				"extra":        map[string]any{"a": 1},
			},
		})
		w.Write(body)
	}))
	defer ts.Close()

	n := New("app_kv_test", "secret_kv_test", ts.URL)
	vars, err := n.GetKV(context.Background(), "device-1")
	if err != nil {
		t.Fatalf("GetKV: %v", err)
	}

	if gotHeaders["X-App-ID"] != "app_kv_test" || gotHeaders["X-Device-ID"] != "device-1" {
		t.Errorf("签名头不完整: %v", gotHeaders)
	}
	if gotHeaders["X-Sign"] == "" || gotHeaders["X-Timestamp"] == "" {
		t.Errorf("缺少签名字段: %v", gotHeaders)
	}
	if vars["announcement"] != "hello" {
		t.Errorf("announcement = %v, want hello", vars["announcement"])
	}
	if v, ok := vars["max_count"].(float64); !ok || v != 42 {
		t.Errorf("max_count = %T %v, want float64 42", vars["max_count"], vars["max_count"])
	}
	if v, ok := vars["enabled"].(bool); !ok || !v {
		t.Errorf("enabled = %T %v, want true", vars["enabled"], vars["enabled"])
	}
	if _, ok := vars["extra"].(map[string]any); !ok {
		t.Errorf("extra 应解析为 map, got %T", vars["extra"])
	}
}

// TestGetKVWithKeys 指定 key 时请求带 ?key= 逗号串
func TestGetKVWithKeys(t *testing.T) {
	var gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("key")
		w.Write([]byte(`{"code":200,"message":"success","data":{"group_link":"https://t.me/x"}}`))
	}))
	defer ts.Close()

	n := New("app_kv_test", "secret_kv_test", ts.URL)
	vars, err := n.GetKV(context.Background(), "device-1", "group_link", "extra")
	if err != nil {
		t.Fatalf("GetKV: %v", err)
	}
	if gotQuery != "group_link,extra" {
		t.Errorf("key query = %q, want group_link,extra", gotQuery)
	}
	if vars["group_link"] != "https://t.me/x" {
		t.Errorf("group_link = %v", vars["group_link"])
	}
}

// TestGetKVServerError 服务端业务错误码应转为 error
func TestGetKVServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code": 500, "message": "boom", "data": null}`))
	}))
	defer ts.Close()

	n := New("app_kv_test", "secret_kv_test", ts.URL)
	if _, err := n.GetKV(context.Background(), "device-1"); err == nil {
		t.Fatal("业务错误码应返回 error")
	}
}

// TestGetKVNoSecret 未配置 app_secret 应直接报错
func TestGetKVNoSecret(t *testing.T) {
	n := New("app_kv_test", "", "http://127.0.0.1:1")
	if _, err := n.GetKV(context.Background(), "device-1"); err == nil {
		t.Fatal("缺少 app_secret 应返回 error")
	}
}
