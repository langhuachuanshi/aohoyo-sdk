package native

import (
	"crypto/sha256"
	"io"
	"os"
)

// executableHash 计算当前可执行文件 SHA256（完整性自检/基线上报）。
func executableHash() ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 256<<20)); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
