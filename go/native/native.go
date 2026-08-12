// Package native 桌面端原生安全模块（Wails/Electron 等可嵌入）。
//
// 设计约束（与 sdk-go 一致）：零三方依赖，仅标准库。
// app_secret 只保存在本模块内，WebView/JS 层永远拿不到——请求签名由原生层完成。
//
// Wails 集成：把 New() 返回的 *Native 实例 Bind 给前端，
// 前端通过 window.__AOHOYO_NATIVE__ 约定的同名方法调用。
package native

import (
	"encoding/hex"
	"fmt"
	"sync"
)

// Native 桌面端安全原语集合。
// 所有方法导出，Wails 可直接绑定；返回结构体均为 JSON 友好类型。
type Native struct {
	mu        sync.RWMutex
	AppID     string
	AppSecret string
	BaseURL   string
	certPin   string
}

// New 创建原生模块实例。appSecret 仅存于原生层，绝不外泄给 JS。
func New(appID, appSecret, baseURL string) *Native {
	return &Native{
		AppID:     appID,
		AppSecret: appSecret,
		BaseURL:   baseURL,
	}
}

// FingerprintResult 机器指纹结果
type FingerprintResult struct {
	Hash   string            `json:"hash"`
	Fields map[string]string `json:"fields"`
}

// GetMachineFingerprint 多信号组合机器指纹（防克隆/伪造）。
// 平台契约：服务端只存 hash，授权绑定用。
func (n *Native) GetMachineFingerprint() (FingerprintResult, error) {
	return machineFingerprint()
}

// RisksResult 风险检测结果
type RisksResult struct {
	Flags []string `json:"flags"`
}

// DetectRisks 检测运行环境风险。flag 取值（与 AS device_record.risk_type 对应）：
// debug / emulator / root / hook / multiopen。
// 注意：这是威慑层自检，可被 patch 绕过；服务端不得仅凭此做安全决策。
func (n *Native) DetectRisks() (RisksResult, error) {
	return detectRisks()
}

// SignResult 请求签名结果（DeviceSign 协议）
type SignResult struct {
	Sign      string `json:"sign"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
}

// SignRequest 按 DeviceSign 协议签名：HMAC-SHA256(app_secret, deviceID+"\n"+timestamp+"\n"+body)。
// body 必须是发送时的原始字节串（JSON 序列化后）。同时生成一次性 nonce 供 X-Nonce 使用。
func (n *Native) SignRequest(deviceID, body string) (SignResult, error) {
	if n.AppSecret == "" {
		return SignResult{}, fmt.Errorf("app_secret 未配置")
	}
	return signRequest(n.AppSecret, deviceID, body)
}

// VerifyUpgrade 校验升级清单签名（HMAC(app_secret, manifestJSON)）。
// manifestJSON 为服务端响应的 data 去掉 signature 字段后的 JSON 字符串。
func (n *Native) VerifyUpgrade(manifestJSON, signature string) (bool, error) {
	if n.AppSecret == "" {
		return false, fmt.Errorf("app_secret 未配置")
	}
	return verifyUpgrade(n.AppSecret, manifestJSON, signature)
}

// IntegrityResult exe 完整性自检结果
type IntegrityResult struct {
	Hash string `json:"hash"`
	OK   bool   `json:"ok"`
}

// SelfIntegrity 计算当前可执行文件 SHA256（启动自检/上报基线用）。
func (n *Native) SelfIntegrity() (IntegrityResult, error) {
	h, err := executableHash()
	if err != nil {
		return IntegrityResult{}, err
	}
	return IntegrityResult{Hash: hex.EncodeToString(h), OK: true}, nil
}

// AcquireMutex 防多开：成功获取互斥锁返回 true；已存在另一实例返回 false。
func (n *Native) AcquireMutex() (bool, error) {
	return acquireMutex(n.AppID)
}

// SetCertPin 设置 TLS 证书固定指纹（由原生 HTTP 层消费）。
func (n *Native) SetCertPin(pin string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.certPin = pin
}

// GetCertPin 读取证书固定指纹。
func (n *Native) GetCertPin() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.certPin
}

// SecureGet 从系统安全存储读取（Windows DPAPI / Unix 0600 文件）。
func (n *Native) SecureGet(key string) (string, error) {
	return secureGet(key)
}

// SecureSet 写入系统安全存储。
func (n *Native) SecureSet(key, val string) error {
	return secureSet(key, val)
}
