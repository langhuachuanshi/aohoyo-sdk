package native

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildSignedCheckBody 按服务端算法构造响应：json.Marshal(去签名结构) → HMAC → 附加 signature 字段。
// 模拟 admin-server service.Upgrade.Check 的签名产出（字节序 = struct 字段序）。
func buildSignedCheckBody(t *testing.T, secret string, r CheckResult) string {
	t.Helper()
	r.Signature = ""
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	sig := hex.EncodeToString(mac.Sum(nil))
	// 服务端 CheckResp signature 是最后字段且非空时输出——等价于在 marshal 结果上拼接尾段
	signed := string(data[:len(data)-1]) + `,"signature":"` + sig + `"}`
	return fmt.Sprintf(`{"code":200,"message":"success","data":%s}`, signed)
}

func TestCheckUpgradeAndVerifySignature(t *testing.T) {
	const secret = "test_secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/as/v1/upgrade/check" {
			t.Errorf("unexpected path %s", req.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body["app_id"] != "demo" || body["platform"] != "windows" {
			t.Errorf("unexpected request body %v", body)
		}
		result := CheckResult{
			HasUpdate: true, LatestVersion: "2026.9.4", LatestVersionCode: 20260904,
			Platform: "windows", DownloadURL: "https://example.com/setup.exe",
			FileSize: 7, SHA256: "abc", UpdateLog: "修复 <问题> & 改进", // 故意含 Go escapeHTML 字符
		}
		w.Write([]byte(buildSignedCheckBody(t, secret, result)))
	}))
	defer srv.Close()

	n := New("demo", secret, srv.URL)
	r, err := n.CheckUpgrade(CheckRequest{CurrentVersionCode: 1, Platform: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasUpdate || r.DownloadURL == "" || r.Signature == "" {
		t.Fatalf("unexpected result %+v", r)
	}
	if r.RawManifest == "" || strings.Contains(r.RawManifest, "signature") {
		t.Fatalf("RawManifest 应为删除 signature 后的原文: %q", r.RawManifest)
	}
	// 字节级 manifest 复算签名（含转义字符场景）必须通过
	if ok, err := n.VerifyCheckResult(r); err != nil || !ok {
		t.Fatalf("签名校验失败 ok=%v err=%v manifest=%q", ok, err, r.RawManifest)
	}

	// 篡改 download_url 后必须失败
	r.RawManifest = strings.Replace(r.RawManifest, "setup.exe", "evil.exe", 1)
	if ok, _ := n.VerifyCheckResult(r); ok {
		t.Fatal("篡改后的 manifest 不应通过校验")
	}
}

func TestCheckUpgradeUnsignedPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"code":200,"message":"success","data":{"has_update":true,"force_update":false,"latest_version":"1.0.0","latest_version_code":10000,"platform":"windows","download_url":"","file_size":0,"md5":"","sha256":"","update_log":""}}`))
	}))
	defer srv.Close()

	n := New("demo", "secret", srv.URL)
	r, err := n.CheckUpgrade(CheckRequest{CurrentVersionCode: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.Signature != "" {
		t.Fatal("未签名响应不应带 signature")
	}
	if ok, err := n.VerifyCheckResult(r); err != nil || !ok {
		t.Fatalf("未开启签名应直接通过: %v %v", ok, err)
	}
}

func TestCheckUpgradeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"code":500,"message":"boom","data":null}`))
	}))
	defer srv.Close()

	n := New("demo", "secret", srv.URL)
	if _, err := n.CheckUpgrade(CheckRequest{CurrentVersionCode: 1}); err == nil {
		t.Fatal("code!=200 应返回错误")
	}
}

func TestDownloadFileResume(t *testing.T) {
	payload := []byte("0123456789abcdef") // 16 字节
	var gotRange string
	full := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		full++
		gotRange = req.Header.Get("Range")
		if r := gotRange; r != "" {
			start := 0
			fmt.Sscanf(r, "bytes=%d-", &start)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			w.Write(payload[start:])
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "setup.exe")
	n := New("demo", "secret", srv.URL)

	// 预置半个文件 → 触发续传
	if err := os.WriteFile(dest+".part", payload[:8], 0o644); err != nil {
		t.Fatal(err)
	}
	var lastRecv, lastTotal int64
	if err := n.DownloadFile(srv.URL, dest, int64(len(payload)), func(recv, total int64) {
		lastRecv, lastTotal = recv, total
	}); err != nil {
		t.Fatal(err)
	}
	if gotRange != "bytes=8-" {
		t.Fatalf("应从 8 续传, got Range=%q", gotRange)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(payload) {
		t.Fatalf("续传拼接结果错误: %q", got)
	}
	if lastRecv != 16 || lastTotal != 16 {
		t.Fatalf("进度回调应到 16/16, got %d/%d", lastRecv, lastTotal)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("完成后 .part 应被 rename 掉")
	}

	// 已下完的 .part 直接落位（不再发请求）
	dest2 := filepath.Join(dir, "setup2.exe")
	if err := os.WriteFile(dest2+".part", payload, 0o644); err != nil {
		t.Fatal(err)
	}
	before := full
	if err := n.DownloadFile(srv.URL, dest2, int64(len(payload)), nil); err != nil {
		t.Fatal(err)
	}
	if full != before {
		t.Fatal(".part 已完整时不应再发网络请求")
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	os.WriteFile(p, []byte("hello"), 0o644)
	sum := sha256.Sum256([]byte("hello"))
	sha := hex.EncodeToString(sum[:])

	n := New("demo", "secret", "")
	if err := n.VerifyFile(p, sha, ""); err != nil {
		t.Fatal(err)
	}
	if err := n.VerifyFile(p, "deadbeef", ""); err == nil {
		t.Fatal("错误 SHA256 应报错")
	}
	if err := n.VerifyFile(p, "", ""); err == nil {
		t.Fatal("无期望值应报错")
	}
}

func TestFilenameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://cdn.example.com/apps/demo/versions/setup-1.3.0.exe": "setup-1.3.0.exe",
		"https://x.com/a/b/c/app-2.0.msi?sign=abc":                   "app-2.0.msi",
		"https://x.com/": "installer.bin",
	}
	for in, want := range cases {
		if got := filenameFromURL(in); got != want {
			t.Errorf("filenameFromURL(%q)=%q want %q", in, got, want)
		}
	}
}
