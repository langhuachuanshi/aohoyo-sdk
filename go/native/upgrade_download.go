package native

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// DownloadProgress 下载进度回调：received 已接收字节（含续传起点），total 总字节（未知为 0）。
type DownloadProgress func(received, total int64)

// DownloadFile 下载安装包到 destPath，断点续传（destPath+".part" 已有内容时发 Range 续传）。
// expectedSize 传服务端 file_size 用于完整性校验；onProgress 可为 nil。
func (n *Native) DownloadFile(url, destPath string, expectedSize int64, onProgress DownloadProgress) error {
	if url == "" {
		return fmt.Errorf("下载地址为空")
	}
	partPath := destPath + ".part"

	var offset int64
	if st, err := os.Stat(partPath); err == nil {
		offset = st.Size()
		// 服务端已知总大小且 .part 已达标 → 直接落位（上次下完但没 rename 的场景）
		if expectedSize > 0 && offset == expectedSize {
			return os.Rename(partPath, destPath)
		}
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	// 大文件下载不用整体超时（仅连接/头阶段受 Transport 控制），靠 body 读取与总量校验兜底
	httpc := &http.Client{Timeout: 0, Transport: &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
	}}
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		// 续传请求被拒（如文件已变更）→ 从头重下
		if offset > 0 {
			return n.freshDownload(url, destPath, partPath, expectedSize, onProgress)
		}
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}

	total := expectedSize
	if total <= 0 {
		if t, ok := parseContentRangeTotal(resp.Header.Get("Content-Range")); ok {
			total = t
		}
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	received, err := pump(f, resp.Body, offset, total, onProgress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if total > 0 && received != total {
		return fmt.Errorf("下载不完整: %d/%d 字节（.part 保留，下次续传）", received, total)
	}
	return os.Rename(partPath, destPath)
}

// freshDownload 丢弃 .part 从头下载（Range 被拒时）。
func (n *Native) freshDownload(url, destPath, partPath string, expectedSize int64, onProgress DownloadProgress) error {
	if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = pump(f, resp.Body, 0, expectedSize, onProgress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(partPath, destPath)
}

func pump(f *os.File, body io.Reader, offset, total int64, onProgress DownloadProgress) (int64, error) {
	buf := make([]byte, 64<<10)
	received := offset
	for {
		nr, er := body.Read(buf)
		if nr > 0 {
			if _, ew := f.Write(buf[:nr]); ew != nil {
				return received, ew
			}
			received += int64(nr)
			if onProgress != nil {
				onProgress(received, total)
			}
		}
		if er == io.EOF {
			return received, nil
		}
		if er != nil {
			return received, er
		}
	}
}

// parseContentRangeTotal 解析 Content-Range: bytes 100-199/300 → 300。
func parseContentRangeTotal(v string) (int64, bool) {
	i := strings.LastIndexByte(v, '/')
	if i < 0 {
		return 0, false
	}
	t, err := strconv.ParseInt(v[i+1:], 10, 64)
	if err != nil || t <= 0 {
		return 0, false
	}
	return t, true
}

// VerifyFile 校验文件哈希：SHA256 优先，其次 MD5；对应期望值为空则跳过该项。
func (n *Native) VerifyFile(path, sha256Hex, md5Hex string) error {
	if sha256Hex == "" && md5Hex == "" {
		return fmt.Errorf("未提供任何哈希期望值")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if sha256Hex != "" {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sha256Hex) {
			return fmt.Errorf("SHA256 不匹配: 期望 %s 实得 %s", sha256Hex, got)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	if md5Hex != "" {
		h := md5.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, md5Hex) {
			return fmt.Errorf("MD5 不匹配: 期望 %s 实得 %s", md5Hex, got)
		}
	}
	return nil
}
