# Changelog

本文件记录 desktop-native-rust（`rust/native`）的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [Unreleased]

### 新增

- **升级执行链（与 go/native 同构）**: `check_upgrade`（POST /as/v1/upgrade/check，按平台取该平台最新可用
  版本——平台独立节奏）+ `verify_check_result`（清单签名复算，字节级提取 manifest 避免跨语言序列化差异，
  未开启签名自动跳过）+ `download_file`（断点续传 Range + 进度回调，续传被拒自动重下）+ `verify_file`
  （SHA256 优先/MD5）+ `install`（Windows msi/exe 静默、Linux deb/rpm/AppImage 自替换、macOS pkg，分离进程
  启动）+ `perform_upgrade` 一站式（on_stage 阶段回调）。新增依赖 ureq（同步 rustls HTTP）与 md-5。
  契约对齐主仓库 upgrade-integration.md（2026-09-04 定稿）

## [0.1.0] - 2026-08-12

### 新增

- **aohoyo-native crate**: 桌面端原生安全模块（Tauri 可集成）——
  机器指纹（Windows MachineGuid / Unix machine-id）、风险检测（debug/emulator/root/hook/multiopen，威慑层）、
  DeviceSign 签名 + nonce、升级清单 HMAC 校验、exe SHA256 完整性、防多开互斥（Global Mutex / flock）、
  DPAPI / 0600 安全存储、证书固定 pin。
- CI：`rust/v*` tag 触发 ubuntu + windows 双平台 cargo test/check
