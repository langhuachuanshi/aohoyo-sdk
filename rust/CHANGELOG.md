# Changelog

本文件记录 aohoyo-native（Rust）的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [Unreleased]

## [0.4.1] - 2026-09-10

### 新增

- **指纹缓存 API（宿主可选）**: `machine_fingerprint_cached(cache_dir)`（命中毫秒级返回，
  未命中真算并写缓存，含算法版本号 `FINGERPRINT_ALGO` 自动失效）+
  `machine_fingerprint_refreshed(cache_dir)`（强制重算并更新缓存）。缓存/后台刷新调度由宿主负责

## [0.4.0] - 2026-09-10

### 变更

- **指纹 v4——全硬件信号 + 注册表/系统 API 直读（⚠️ 旧指纹全部失效，绑定需重绑）**:
  ① 性能：弃用 PowerShell/WMI（2~5s），改注册表 RegGetValueW + GetPhysicallyInstalledSystemMemory
  直读（毫秒级）② 信号：MachineGuid + hostname + 主板/BIOS 序列号（BIOS 键）+ CPU Identifier +
  显卡 DriverDesc 列表（枚举显示类子键，过滤驱动未装占位）+ 物理内存字节
  ③ 哈希改 MD5（32 位 hex，用户可读性优先；非安全场景）
  与 go/native fingerprint_windows.go 严格同步

## [0.3.2] - 2026-09-10

### 变更

- **机器指纹 v3（⚠️ hash 会变）**: Windows 全硬件信号——MachineGuid + hostname + 主板序列号 +
  BIOS 序列号 + CPU ProcessorId + 显卡名列表（排序拼接，过滤驱动未装占位）+ 内存总容量字节。
  PowerShell Get-CimInstance 一次调用读取（零三方依赖，启动时算一次）；占位值自动跳过。
  与 go/native fingerprint_windows.go 严格同步。换/加显卡内存等硬件变更 → 指纹变（产品口径：
  换硬件 = 新设备，需重新授权）。升级后旧指纹失效，绑定需重绑

### 新增

- **CheckResult 新增 `mirror_download_url`（UPG-1 镜像契约）**: 镜像解析端点（302 直链或回退主源），未镜像为空（serde default，旧服务端响应兼容）；下载候选链消费待蓝奏云接入后落地

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
