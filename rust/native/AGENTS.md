# desktop-native-rust 开发规范

## 项目定位

Tauri（或任意 Rust 桌面壳）的原生模块，与平台「应用安全 + 应用升级」配套。
与 [`go/native`](../../go/AGENTS.md) 同构（API 命名 Go 大驼峰 / Rust snake_case，行为语义一致）；
与 sdk-js 的 `window.__AOHOYO_NATIVE__` 契约一一对应。**app_secret 只存在于本 crate，JS 永远拿不到。**

升级契约见[主仓库 `docs/specs/upgrade-integration.md`](https://github.com/langhuachuanshi/aohoyo)（2026-09-04 定稿：
平台独立节奏 + 分平台灰度 + 清单签名）。

## 技术栈

Rust 1.75+，crate 名 `aohoyo-native`。依赖刻意精简：serde/sha2/hmac/hex/rand/libc +
ureq（同步 HTTP，rustls，无 tokio）+ md-5（哈希校验）+ zip（Windows zip 包解压自替换，仅 deflate 特性）。**不加 async 运行时**——桌面壳（Tauri）自带调度，
本 crate 保持阻塞式小函数，避免传染依赖树。

## 架构

```
src/
├── lib.rs        ← Native 结构体 + 公共类型（含 CheckRequest/CheckResult，字段序与服务端严格一致）
├── fingerprint.rs ← 机器指纹
├── risks.rs      ← 风险检测（威慑层）
├── sign.rs       ← DeviceSign 签名 + 升级清单 HMAC 校验
├── upgrade.rs    ← 升级检测（check_upgrade）+ manifest 字节级提取/验签 + exe 自检
├── download.rs   ← 断点续传下载 + SHA256/MD5 文件校验
├── install.rs    ← 安装器分离启动（Win msi/exe/zip、Linux deb/rpm/AppImage、macOS pkg）+ perform_upgrade
├── zip_install.rs ← Windows zip 包解压自替换（剥壳/主 exe 识别/防 Zip Slip/换血脚本）
├── mutex.rs      ← 防多开
└── secure.rs     ← DPAPI / 0600 安全存储
```

## 关键设计约束

1. **CheckResult 字段声明序与服务端 CheckResp 严格一致**（签名相关，勿调整顺序）。
2. **manifest 验签走字节级提取**（`extract_manifest`：响应原文切 data 段删 signature 尾段），
   不走反序列化重序列化——避免 Go/Rust 键序与 escapeHTML 差异导致验签失败。
3. 安装一律分离进程（spawn 后不 wait），宿主收到 `install_launched=true` 后自行退出。
4. Windows exe 自替换采用「旧文件改名保留 → 新文件落位」经典模式，不做运行中覆盖。

## 发布

`rust/v*` tag → CI cargo test（ubuntu + windows 矩阵）。打 tag 前四项检查见[根 AGENTS.md](../../AGENTS.md)。
