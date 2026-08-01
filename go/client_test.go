package aohoyo

import (
	"testing"
)

func TestNew_Success(t *testing.T) {
	c, err := New("APP_x", "secret", "https://admin.example.com")
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if c.AppID != "APP_x" || c.AppSecret != "secret" {
		t.Error("基础字段未正确保存")
	}
	if c.S2S == nil {
		t.Error("S2S 客户端未初始化")
	}
	if c.Stats == nil {
		t.Error("Stats 客户端未初始化")
	}
}

func TestNew_Validation(t *testing.T) {
	cases := []struct {
		name     string
		appID    string
		secret   string
		baseURL  string
		wantErr  bool
	}{
		{"空 appID", "", "s", "https://x", true},
		{"空 secret", "APP_x", "", "https://x", true},
		{"空 baseURL", "APP_x", "s", "", true},
		{"全合法", "APP_x", "s", "https://x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.appID, tc.secret, tc.baseURL)
			if (err != nil) != tc.wantErr {
				t.Errorf("New() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// TestSubClients_ConfigSync 验证子客户端继承了统一入口的配置
func TestSubClients_ConfigSync(t *testing.T) {
	c, _ := New("APP_x", "secret", "https://admin.example.com/")

	if c.S2S.AppID != "APP_x" {
		t.Errorf("S2S.AppID = %q, want APP_x", c.S2S.AppID)
	}
	if c.S2S.AppSecret != "secret" {
		t.Errorf("S2S.AppSecret 未同步")
	}
	if c.Stats.AppID != "APP_x" {
		t.Errorf("Stats.AppID = %q, want APP_x", c.Stats.AppID)
	}
}
