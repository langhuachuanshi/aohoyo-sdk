package native

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeTestZip 构造测试 zip：entries 为 name/content；目录条目 content 传 "" 且 name 以 / 结尾。
func writeTestZip(t *testing.T, path string, entries []struct{ Name, Content string }) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range entries {
		var fw io.Writer
		if strings.HasSuffix(e.Name, "/") {
			fw, err = w.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Deflate})
		} else {
			fw, err = w.Create(e.Name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.Content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeEntryName(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"a/b/c.txt", "a/b/c.txt", false},
		{`a\b.txt`, "a/b.txt", false},
		{"dir/", "dir", false},
		{"../evil.txt", "", true},
		{"a/../../evil.txt", "", true},
		{"/abs/path.txt", "", true},
		{"C:/evil.txt", "", true},
		{"C:evil.txt", "", true},
		{`a\..\..\evil.txt`, "", true},
	}
	for _, c := range cases {
		got, err := sanitizeEntryName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("sanitizeEntryName(%q) 应报错，得到 %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("sanitizeEntryName(%q) 不应报错: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("sanitizeEntryName(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestCommonRootPrefix(t *testing.T) {
	type ent = struct {
		Name  string
		IsDir bool
	}
	cases := []struct {
		name string
		ents []ent
		want string
	}{
		{"顶层唯一目录", []ent{{"app/app.exe", false}, {"app/lib/data.txt", false}, {"app/", true}}, "app/"},
		{"根下有散文件", []ent{{"app.exe", false}, {"lib/data.txt", false}}, ""},
		{"多个顶层", []ent{{"a/1.txt", false}, {"b/2.txt", false}}, ""},
		{"根文件+目录", []ent{{"readme.txt", false}, {"app/1.txt", false}}, ""},
	}
	for _, c := range cases {
		got, err := commonRootPrefix(c.ents)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: commonRootPrefix = %q, 期望 %q", c.name, got, c.want)
		}
	}
}

func TestExtractZipStripsTopDir(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "t.zip")
	writeTestZip(t, zipPath, []struct{ Name, Content string }{
		{"myapp/app.exe", "MZ"},
		{"myapp/bin/helper.exe", "HZ"},
		{"myapp/data.ini", "cfg"},
	})
	dest := filepath.Join(tmp, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(dest, "app.exe")) {
		t.Fatal("app.exe 应被解压到根（顶层目录剥壳）")
	}
	if !fileExists(filepath.Join(dest, filepath.Join("bin", "helper.exe"))) {
		t.Fatal("bin/helper.exe 应保留子目录结构")
	}
	if b, err := os.ReadFile(filepath.Join(dest, "app.exe")); err != nil || string(b) != "MZ" {
		t.Fatalf("app.exe 内容不符: %v %q", err, string(b))
	}
	if fileExists(filepath.Join(dest, "myapp")) {
		t.Fatal("顶层目录应被剥壳")
	}
}

func TestFindMainExeRules(t *testing.T) {
	tmp := t.TempDir()

	// 无 exe
	if err := os.WriteFile(filepath.Join(tmp, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := findMainExe(tmp, "myapp"); err == nil {
		t.Fatal("无 exe 应报错")
	}
	os.Remove(filepath.Join(tmp, "readme.txt"))

	// 唯一 exe（大小写不敏感扩展名）
	if err := os.WriteFile(filepath.Join(tmp, "Main.EXE"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := findMainExe(tmp, "myapp"); err != nil || got != "Main.EXE" {
		t.Fatalf("唯一 exe 应直接采用: %q %v", got, err)
	}
	os.Remove(filepath.Join(tmp, "Main.EXE"))

	// 多 exe：按包名匹配
	for _, name := range []string{"myapp.exe", "uninstall.exe"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := findMainExe(tmp, "myapp"); err != nil || got != "myapp.exe" {
		t.Fatalf("应按包名识别主 exe: %q %v", got, err)
	}

	// 多 exe 且无同名 → 报错
	if _, err := findMainExe(tmp, "other"); err == nil {
		t.Fatal("多 exe 无同名应报错")
	}
}

func TestIsSingleFilePackage(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "myapp.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSingleFilePackage(tmp) {
		t.Fatal("单文件应判定为 true")
	}
	if err := os.WriteFile(filepath.Join(tmp, "cfg.ini"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isSingleFilePackage(tmp) {
		t.Fatal("多文件应判定为 false")
	}
	os.Remove(filepath.Join(tmp, "cfg.ini"))
	if err := os.Mkdir(filepath.Join(tmp, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if isSingleFilePackage(tmp) {
		t.Fatal("含子目录应判定为 false")
	}
}

func TestBuildUpgradeScript(t *testing.T) {
	s := buildUpgradeScript(`C:\Program Files\MyApp`, `C:\Program Files\MyApp.update-1-2`, "myapp.exe")
	for _, want := range []string{
		`set "APP=C:\Program Files\MyApp"`,
		`set "NEW=C:\Program Files\MyApp.update-1-2"`,
		`set "EXE=C:\Program Files\MyApp\myapp.exe"`,
		"chcp 65001",
		`if /i not "%%~fD"=="C:\Program Files\MyApp.update-1-2"`,
		`move /y "%APP%" "%APP%.old"`,
		`robocopy "%NEW%" "%APP%" /e /purge /r:2 /w:1`,
		`start "" "%EXE%"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("脚本缺少: %s", want)
		}
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Fatal("脚本行尾应为 CRLF")
	}
	nested := buildUpgradeScript(`C:\App`, `C:\App.update-1-2`, "bin/app.exe")
	if !strings.Contains(nested, `set "EXE=C:\App\bin\app.exe"`) {
		t.Fatal("子目录 exe 应以反斜杠拼接")
	}
}

func TestInstallZipNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅非 Windows 验证暂不支持分支")
	}
	if err := installZip("/nonexistent.zip"); err == nil {
		t.Fatal("非 Windows 应返回暂不支持错误")
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
