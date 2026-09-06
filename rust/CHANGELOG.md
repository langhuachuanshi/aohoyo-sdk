# Changelog

本文件记录 aohoyo-native（Rust）的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [Unreleased]

## [0.1.1] - 2026-09-06

### 修复

- **Windows 编译错误**: `zip_install.rs` 单文件包换血路径 `staging`（String）误调 `Path::join`，
  Linux 下位于 cfg(windows) 块外不可见导致漏检；改为 `Path::new(&staging).join(...)`

## [0.1.0] - 2026-09-06（首个 tag）

### 新增

- **安全原语**: `machine_fingerprint`（机器指纹）/ `detect_risks`（风险检测威慑层）/ `sign_request`
  （DeviceSign 签名 + nonce）/ `verify_upgrade`（升级清单 HMAC 校验）/ `self_integrity`（exe SHA256 自检）/
  `acquire_mutex`（防多开）/ `secure_get|secure_set`（DPAPI / 0600 文件安全存储）/ `set_cert_pin`（证书固定）
- **升级执行链**（与 go/native 同构，契约见主仓库 `docs/specs/upgrade-integration.md`）:
  `check_upgrade`（按平台取该平台最新可用版本）+ `verify_check_result`（字节级 manifest 签名复算）+
  `download_file`（Range 断点续传 + 进度回调）+ `verify_file`（SHA256/MD5）+ `install`（Windows msi/exe 静默
  + zip 解包自替换；Linux deb/rpm/AppImage；macOS pkg，分离进程）+ `perform_upgrade` 一站式（on_stage 阶段回调）
- **下载地址安全门**: `download_file` 默认仅接受 `https://` 安装包地址，`set_allow_http(true)` 显式放行
  http（本地调试语义）；`file:`/`ftp:` 等其他 scheme 任何情况拒绝
