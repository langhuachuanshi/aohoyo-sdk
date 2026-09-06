package native

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAdsSigned(t *testing.T) {
	var gotHeaders http.Header
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"code":200,"message":"success","data":{"positions":{"splash":[{"id":7,"position_id":2,"title":"活动","content":"立减","image_url":"https://cdn/x.png","link_url":"https://x.com","ad_type":4,"width":1080,"height":1920,"start_time":null,"end_time":null,"config":{"freq":1}}]}}}`))
	}))
	defer srv.Close()

	n := New("demo", "secret", srv.URL)
	ads, err := n.GetAds(context.Background(), "device-1", "splash")
	if err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get("X-Device-Sign") == "" || gotHeaders.Get("X-Device-ID") != "device-1" || gotHeaders.Get("X-App-ID") != "demo" {
		t.Fatalf("应带 DeviceSign 头: %v", gotHeaders)
	}
	if gotQuery != "app_id=demo&position=splash" {
		t.Fatalf("query 错误: %s", gotQuery)
	}
	if len(ads.Positions["splash"]) != 1 {
		t.Fatalf("应返回 1 条广告: %+v", ads.Positions)
	}
	item := ads.Positions["splash"][0]
	if item.ID != 7 || item.AdType != 4 || item.ImageURL == "" || item.Config["freq"].(float64) != 1 {
		t.Fatalf("广告项解析错误: %+v", item)
	}
}

func TestAdReportPlain(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
	}))
	defer srv.Close()

	n := New("demo", "secret", srv.URL)
	if err := n.RecordImpression(context.Background(), 7, "device-1"); err != nil {
		t.Fatal(err)
	}
	if path != "/as/v1/ads/impression" || body["ad_id"].(float64) != 7 || body["device_id"] != "device-1" {
		t.Fatalf("曝光上报错误: %s %v", path, body)
	}
	if err := n.RecordClick(context.Background(), 7, "device-1"); err != nil {
		t.Fatal(err)
	}
	if path != "/as/v1/ads/click" {
		t.Fatalf("点击上报路径错误: %s", path)
	}
}
