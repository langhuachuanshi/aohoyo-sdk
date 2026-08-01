package s2s

import (
	"context"
	"time"
)

// HTTP 请求超时配置（v0.9.0 起 HTTPClient.Timeout=0，由方法内部 context 管超时）。
//
// 背景：旧版 HTTPClient.Timeout 硬编码 30s，七牛直传是单次 POST 把整个文件传完，
// 200MB 文件在家庭上行（3MB/s）下需 ~67s，撞 30s 线被强制取消 → 重试死循环。
// 改为：HTTPClient.Timeout=0（无总超时），每个方法按请求类型自己包 context 超时：
//   - 短请求（GetUploadToken/Delete）：固定 shortRequestTimeout
//   - 上传请求（Upload/directPOST/directPOSTStream/batch）：uploadBaseTimeout + 按文件大小估算
const (
	// shortRequestTimeout 短请求超时（拿凭证、删除等，body 几十 KB，30s 绰绰有余）。
	shortRequestTimeout = 30 * time.Second

	// uploadBaseTimeout 上传请求的基础超时（覆盖连接建立 + 小文件余量）。
	uploadBaseTimeout = 30 * time.Second

	// uploadBytesPerSec 上传速率估算（1MB/s），用于按文件大小算额外超时。
	// 取家庭宽带上行的保守值：3MB/s 上行对应 ~1MB/s 的「业务有效吞吐」余量估算，
	// 留足重传/网络抖动空间。200MB 文件 → 30s + 200s = 230s（实际 3MB/s 传完仅需 67s）。
	uploadBytesPerSec int64 = 1 << 20 // 1MB
)

// withShortTimeout 给短请求派生一个固定 shortRequestTimeout 的子 context。
//
// 用法：ctx, cancel := withShortTimeout(ctx); defer cancel()
// 调用方传入的 ctx 若先于 shortRequestTimeout 结束（如调用方主动取消），子 ctx 会随之结束。
func withShortTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, shortRequestTimeout)
}

// withUploadTimeout 给上传请求派生一个按文件大小动态算超时的子 context。
//
// 超时 = uploadBaseTimeout + sizeBytes/uploadBytesPerSec 秒（按 1MB/s 估算）。
// 例子：1MB → 31s，10MB → 40s，200MB → 230s。
//
// 用法：ctx, cancel := withUploadTimeout(ctx, int64(len(data))); defer cancel()
// sizeBytes <= 0 时退化为 uploadBaseTimeout（防御性，正常调用都传真实大小）。
func withUploadTimeout(ctx context.Context, sizeBytes int64) (context.Context, context.CancelFunc) {
	extra := time.Duration(0)
	if sizeBytes > 0 {
		extra = time.Duration(sizeBytes/uploadBytesPerSec) * time.Second
	}
	return context.WithTimeout(ctx, uploadBaseTimeout+extra)
}
