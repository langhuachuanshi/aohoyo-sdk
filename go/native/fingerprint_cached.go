package native

// 指纹缓存层（宿主可选，与 rust/native 同构）：启动毫秒级 + 后台刷新由宿主调度。

import (
	"os"
	"path/filepath"
)

const (
	// FingerprintAlgo 指纹算法版本号——写入缓存，算法/字段变更时递增使旧缓存自动失效。
	FingerprintAlgo = "fp-v4-md5"
	cacheFile       = "fingerprint.cache"
)

// MachineFingerprintCached 带缓存的指纹：cacheDir 下命中（算法版本一致）直接返回（毫秒级）；
// 未命中/版本不符/损坏 → 真算并写缓存。
// 返回 (指纹, 是否来自缓存)。宿主可另用 machineFingerprint 真算比对，或
// MachineFingerprintRefreshed 强制重算并更新缓存。
func MachineFingerprintCached(cacheDir string) (FingerprintResult, bool, error) {
	path := filepath.Join(cacheDir, cacheFile)
	if text, err := os.ReadFile(path); err == nil {
		var algo, hash string
		parseKV(string(text), func(k, v string) {
			switch k {
			case "algo":
				algo = v
			case "hash":
				hash = v
			}
		})
		if algo == FingerprintAlgo && hash != "" {
			return FingerprintResult{Hash: hash, Fields: map[string]string{}}, true, nil
		}
	}
	fp, err := machineFingerprint()
	if err != nil {
		return FingerprintResult{}, false, err
	}
	_ = os.WriteFile(path, []byte("algo="+FingerprintAlgo+"\nhash="+fp.Hash+"\n"), 0o644)
	return fp, false, nil
}

// MachineFingerprintRefreshed 强制真算并更新缓存（宿主后台刷新/换硬件检出用）。
func MachineFingerprintRefreshed(cacheDir string) (FingerprintResult, error) {
	fp, err := machineFingerprint()
	if err != nil {
		return FingerprintResult{}, err
	}
	path := filepath.Join(cacheDir, cacheFile)
	_ = os.WriteFile(path, []byte("algo="+FingerprintAlgo+"\nhash="+fp.Hash+"\n"), 0o644)
	return fp, nil
}

// parseKV 逐行解析 "k=v"。
func parseKV(text string, fn func(k, v string)) {
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || text[i] == '\n' {
			line := text[start:i]
			start = i + 1
			for j := 0; j < len(line); j++ {
				if line[j] == '=' {
					fn(line[:j], line[j+1:])
					break
				}
			}
		}
	}
}
