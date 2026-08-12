# Changelog

本文件记录 desktop-native-rust（`rust/native`）的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [0.1.0] - 2026-08-12

### 新增

- **aohoyo-native crate**: 桌面端原生安全模块（Tauri 可集成）——
  机器指纹（Windows MachineGuid / Unix machine-id）、风险检测（debug/emulator/root/hook/multiopen，威慑层）、
  DeviceSign 签名 + nonce、升级清单 HMAC 校验、exe SHA256 完整性、防多开互斥（Global Mutex / flock）、
  DPAPI / 0600 安全存储、证书固定 pin。
- CI：`rust/v*` tag 触发 ubuntu + windows 双平台 cargo test/check
