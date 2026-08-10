# Changelog

本文件记录 sdk/server-go 的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [0.10.0] - 2026-08-10

### 破坏性变更（Breaking）

- **路由前缀重构**: 对齐后端服务前缀架构
  - s2s 存储: `/v1/storage/*` → `/as/v1/storage/*`
  - stats 统计: `/v1/stats/events` → `/as/v1/stats/events`
  - uc 用户校验: `/v1/auth/verify`、`/v1/users` → `/uc/v1/auth/verify`、`/uc/v1/users`
- `BaseURL` 不再需要带 `/api`，直接用域名即可

## [0.9.0] - 2026-07-20

### 修复

- **大文件直传 30s 超时死循环**：HTTPClient.Timeout 硬编码 30s，七牛直传是单次 POST 把整个
  文件传完，200MB 文件在家庭上行（3MB/s）下需 ~67s，撞 30s 线被强制取消 → `directPOSTStream`/
  `directPOST` 重试循环触发 → 从头重传 → 又 30s 超时 → 重试耗尽报失败（090cq workbench 实测：
  200MB 文件进度稳定卡在 50% 反复从 0 重试）

### 变更

- **HTTPClient.Timeout 改为 0**（不再设总超时），改由方法内部 `context.WithTimeout` 管超时
  （s2s 顶层 + s2s 子包 + stats 子包三处 `New()` 同步改）
- 新增 `s2s/timeout.go`：`withShortTimeout`（短请求固定 30s）/ `withUploadTimeout`（上传按
  文件大小动态算）辅助函数
- **超时分两类**：
  - 短请求（GetUploadToken / Delete / stats.ReportEvents）：固定 30s（stats 仍 10s）
  - 上传（Upload 代理 / directPOST / directPOSTStream / batch.uploadSingle）：
    `30s + len(data)/1MB × 1s`，即 1MB→31s，200MB→230s（够 3MB/s 上行 67s 传完）
- 上传超时包在重试循环**外**：总时长超时则整个方法失败（不再重试），避免无限重试

### 兼容性

- **非 breaking**：不改公开 API 签名。`HTTPClient` 仍是导出字段，调用方自定义的 HTTPClient
  （含自定义 Timeout）仍生效——090cq 撤掉本地补丁后会落到 SDK 新默认（Timeout=0 + 方法内 context）
- 超时只放宽（30s → 动态更长），不收紧；现有短请求仍 30s 内完成
- 调用方传入的 ctx 不再被 SDK 内部 30s 抢先超时，语义更清晰

### 说明

- 5 个新单测覆盖：动态超时公式（200MB/100MB/10MB/1MB/0/负数 6 例）+ 父 ctx 取消传播 +
  大文件直传不重试（流式 + 非流式）+ deadline 实际生效
- 触发场景：090cq workbench 最近开始传 200MB+ 工具包，撞上 30s 超时线（之前主要传小文件
  CHM/图标，30s 够用所以一直没暴露）

## [0.8.0] - 2026-07-19

### ⚠️ BREAKING — S2S / Stats 客户端 BaseURL 语义变更

去掉 SDK 内硬编码的 `/api` 前缀，把 `/api` 的责任集中到 `BaseURL` 一处，
根治与 api.aohoyo.com 的 Caddy 反代「双前缀」404。

**背景**：090cq workbench 生产环境 CDN 上传全部 404。
SDK 拼 `c.BaseURL+"/api/storage/upload-token"`，经 Caddy 兜底规则
`handle { rewrite * /api{uri}; reverse_proxy aohoyo-as:33800 }`
再补一层 `/api` → `/api/api/storage/upload-token` → AS 无此路由 → `404 page not found`。
容器内直连不经 Caddy 所以正常，本质是「SDK 硬编码」和「Caddy 兜底补前缀」两处重复，
属于契约二义性。

**新契约**：`BaseURL` 永远指向「admin-server 的 `/api` 路由组根」，
SDK 只拼 `/storage/...`（stats 只拼 `/stats/events`）到具体接口，不再含 `/api`：

- 容器内直连：`BaseURL = http://aohoyo-as:33800/api`
- 外网经反代：`BaseURL = https://api.example.com`（Caddy 补 `/api` 后等价于上面）

两种场景对 SDK 完全透明，经反代时绝不再双前缀。
admin-server 的路由组 `engine.Group("/api/storage")` 保持原样，Caddyfile / admin-web / swagger 都不动。

### 变更

- `s2s.Client`：`Upload` / `GetUploadToken` / `Delete`（client.go）+ 批量回退上传（batch.go）
  URL 拼接从 `c.BaseURL+"/api/storage/*"` 改为 `c.BaseURL+"/storage/*"`
- `stats.Client`：`endpointEvents` 从 `/api/stats/events` 改为 `/stats/events`
- 同步更新 `Client` / `New` / 各方法注释，写明 BaseURL 指向 `/api` 路由组根
- 同步更新 `s2s/*_test.go`（prefix/token_upload/batch）与 `stats/stats_test.go`
  的 mock server 路径断言（去掉 `/api` 前缀，否则测试会挂）

### 升级指南（BREAKING，调用方必须改一处）

- **容器内直连**：`AS_BASE_URL` 从 `http://host:33800` 改为 `http://host:33800/api`
- **外网经反代**：`AS_BASE_URL=https://api.example.com` **保持不变**（反代规则已隐含 `/api`）
- 仅改环境变量一处，代码无需改动
- stats 包与 s2s 共用同一个 `BaseURL`，语义同步变更

### 说明

- 与上游（admin-server 维护方）确认采用方案 A：BaseURL 集中负责 `/api`，SDK 不管前缀
- 不影响 AS 路由、Caddyfile、admin-web、swagger

## [0.7.0] - 2026-07-18

### 新增

- **`s2s.UploadByTokenWithProgress`**：凭证直传 + 字节级进度回调
  - workbench 一键上传等场景显示上传百分比（原 `UploadByToken` 无进度，前端只能 loading）
  - 进度语义对齐 lanzou-go v0.3.0 / baidupan-go v0.5.0：`onProgress(uploaded, total int64)`，
    `total=len(data)`，单调递增，最终等于 total
  - 仅在网络传输阶段触发（非「读到内存」进度）

### 变更

- 直传 body 构造改流式：`io.Pipe` + `multipart.NewWriter` + `progressReader`，
  HTTP client 边读边发，进度随网络传输实时回调（参考 lanzou-go 的 `postMultipartStream`）
- 流式重试每次重新开 pipe + 重写 multipart（`io.Pipe` reader 只能读一次，body 需重构）
- 429/5xx 退避重试逻辑与原 `directPOST` 一致（复用 `MaxRetries` + `parseRetryAfter`）

### 兼容性

- 原 `UploadByToken` / `directPOST` / `buildDirectPOSTBody` 一字未改，向后完全兼容
- `UploadByTokenWithProgress` 在 `onProgress==nil` 时直接转调 `UploadByToken`，行为字节级一致
- proxy 模式（本地存储等）回退 `Upload`，无进度回调（上传快，可接受）
- PUT 模式（OSS/COS 预签名）暂未实现，返回错误（同 `UploadByToken`）

### 说明

- 三平台进度体验统一：七牛（aohoyo）+ 百度（baidupan-go）+ 蓝奏（lanzou-go）均有字节级回调
- 4 个新单测：进度回调（单调递增 + 最终到 total）/ 流式重试（429→200 body 重构）/ nil 转调 /
  proxy 回退（无回调）

## [0.6.0] - 2026-07-07

### 新增

- **`s2s.UploadBatchByPrefix`**：前缀凭证批量直传（1 个凭证传整个目录）
  - CHM 整站场景：900 文件从 900 次拿凭证降到 1 次，30 分钟 → < 2 分钟
  - 七牛 bucket scope（能覆盖），重新上传同站点覆盖旧文件
  - 每文件 key = KeyPrefix + FileName（或 item.Key 指定）
  - 复用 uploadBatchGeneric 的 worker 池 + 节流 + 进度 + 429 重试
  - proxy 模式自动回退 UploadBatch（本地存储等）

### 变更

- **AS 端 `GenerateUploadToken` 加 mode 参数**：
  - `mode="prefix"`：签 bucket 级凭证（Fields 不含 key，加 KeyPrefix）
  - `mode=""` 或 `"key"`：单 key 凭证（现有行为不变）
- AS 新增 `QiniuStorage.UploadTokenForBucket`（bucket scope 凭证）
- `UploadDirective` 加 `KeyPrefix` 字段（批量模式返回前缀）
- `GetUploadToken` 加 mode 参数
- `directPOST` / `buildDirectPOSTBody` 加 overrideKey（批量模式每文件不同 key）

### 说明

- 用 bucket scope 而非七牛前缀 scope（IsPrefixalScope）：后者强制 insertOnly 不能覆盖
- bucket scope 安全权衡：凭证泄露能写整个 bucket，靠 1h TTL + S2S 鉴权缓解
- 3 个新单测：批量直传（1 次凭证 + 10 文件不同 key）/ 自定义 key / proxy 回退

## [0.5.0] - 2026-07-07

### 新增

- **`s2s.UploadByToken`**：凭证直传单个文件（不经 AS 代理，直传对象存储）
  - 先调 GetUploadToken 拿凭证，按 AS 返回的 UploadDirective 直传
  - mode=direct + POST：七牛表单直传 upload.qiniup.com（token + key + file）
  - mode=proxy：自动回退 Upload() 代理上传（本地存储等）
  - 直传含 429/5xx 退避重试（复用 MaxRetries）
- **`s2s.UploadBatchByToken`**：批量凭证直传（worker 池 + 节流 + 进度）
  - 复用 UploadBatch 的并发/节流/进度脚手架（抽取 uploadBatchGeneric）
  - 每文件独立拿凭证 + 直传，proxy 模式自动回退代理
- **`UploadDirective` 结构**：服务商无关的直传指令（Method/Endpoint/Fields/Headers/FileField）

### 变更

- **AS 端 `UploadTokenResult` 重构为服务商无关格式**：
  - 新增 Provider 字段 + Upload 嵌套（UploadDirective）
  - 保留旧 Token/Domain 字段向后兼容 admin-web
  - GenerateUploadToken 按 provider.Type switch 分派（七牛 POST，其他 proxy）
- 抽取 `uploadBatchGeneric`：批量上传引擎与 singleFn 解耦，代理/凭证两种模式共用

### 说明

- 七牛 POST 直传已完整实现；OSS/COS 的 PUT 预签名留扩展位（本次返回 proxy）
- sdk PUT 直传分支已预留，返回 "暂未实现" 提示
- 4 个新单测覆盖：单文件直传 / proxy 回退 / 批量直传 / 批量 proxy 回退

## [0.4.0] - 2026-07-07

### 新增

- **`s2s.UploadBatch`**：并发批量上传文件（CHM 转站点等数百文件场景）
  - worker 池并发（默认 5，`Client.MaxConcurrency` 可配）
  - **客户端节流**：令牌桶限速，默认 9 次/秒（`Client.RatePerSec` 可配），
    避免并发瞬间打爆 AS 限流（600/min = 10/s，留 1 余量）
  - **429 自动重试**：单文件遇到限流时指数退避重试（默认 3 次，`Client.MaxRetries` 可配），
    支持读 AS 的 Retry-After 头
  - 进度回调 `onProgress(done, total)`（atomic 计数，并发安全）
  - 单文件失败不中断整体，返回 `BatchResult`（URLs/Success/Failed/Errors）
  - context 取消时停止派发新任务
  - 6 个单测覆盖：全部成功 / 部分失败 / 空列表 / 取消 / 429 重试 / 重试耗尽
  - `-race` 检测无数据竞争

### 变更

- AS 端 `/api/storage/upload` 限流改为分层：S2S 按 app_id 限流 600/min（支持批量上传），JWT 用户保持 20/min
- `s2s.Client` 新增 `MaxConcurrency`/`RatePerSec`/`MaxRetries` 字段

## [0.3.0] - 2026-07-07

### 新增

- **根包 `aohoyo`**：统一入口 Client，一次初始化拿全部能力
  - `aohoyo.New(appID, appSecret, baseURL)` 返回组合客户端
  - `c.S2S` 暴露存储操作（上传/删除/直传凭证）
  - `c.Stats` 暴露统计上报（事件上报）
  - 复用统一 HTTP 连接池，避免多客户端各自建 transport
  - 参数校验（appID/appSecret/baseURL 非空）
- 3 个单测覆盖初始化成功/失败、子客户端配置同步

### 说明

- 子包 `s2s` / `stats` 不变，仍可独立使用（向后兼容 v0.1.0 / v0.2.0）
- 推荐新接入方用统一入口，避免重复初始化

## [0.2.0] - 2026-07-07

### 新增

- **`stats` 包**：统计事件上报客户端，对应 AS 的公开接口 `POST /api/stats/events`
  - `stats.Client.ReportEvents`：批量上报（1-50 条，自动补 AppID/ClientTS）
  - `stats.Client.ReportEvent`：单条便捷封装
  - 事件类型/平台常量（`EventCustom`、`PlatformWindows` 等），与服务端 oneof 校验对齐
  - 4 个单测覆盖 mock server 验证、边界校验、单条封装、工具函数

### 说明

- `stats` 走公开接口，无需 app_secret 签名，与 `s2s` 包互补
- `s2s` 包本版无变化（仍为 v0.1.0 的实现）

## [0.1.0] - 2026-07-07

### 新增

- **首个版本**：S2S 签名客户端，供外部 Go 服务（wilas 等）调用 AS 对外存储接口
- `s2s.Sign` / `Verify` / `SignHeaders`：纯签名函数，HMAC-SHA256 + 防重放时间戳
- `s2s.Client.Upload`：multipart 上传文件到 `/api/storage/upload`
- `s2s.Client.GetUploadToken`：获取七牛直传凭证
- `s2s.Client.Delete`：批量删除文件
- 3 个单测覆盖签名闭环（自签自验 + 篡改必败 + 稳定性）

### 备注

- 与 admin-server 的 `JWTOrS2S` 双鉴权中间件配套，同一套 `/api/storage/*` 接口 JWT/S2S 共用
- module 名 `github.com/aohoyo/sdk-go` 独立于目录名，未来拆仓库不影响引用方
