# sdk/server-go 开发规范

## 项目定位

服务端 Go SDK + 桌面端原生安全模块（`native/`）。SDK 给外部 Go 后端（wilas 等）集成，
用 S2S 签名调 AS 对外接口（`/as/v1/storage/*`）；`native/` 给 Wails 桌面壳嵌入（见 `native/README.md`）。

与 [`js`](../js/AGENTS.md) 区别：client-js 是客户端 SDK（JWT），server-go 是服务端 SDK（S2S 签名）。

S2S 协议见[主仓库 `docs/specs/s2s-sign.md`](https://github.com/langhuachuanshi/aohoyo)。

## 技术栈

Go 1.21+，**零三方依赖**（仅标准库），module：`github.com/langhuachuanshi/aohoyo-sdk/go`

## 架构

```
├── client.go       ← 统一入口 aohoyo.New(appID, secret, baseURL)，组合 s2s + stats + uc
├── s2s/            ← S2S 签名 + 存储客户端（Upload/GetUploadToken/Delete）
├── stats/          ← 统计上报
├── uc/             ← 用户中心 Token 验证（Bearer 透传）
└── native/         ← 桌面端原生安全模块（Wails 可绑定；指纹/风险检测/DeviceSign 签名/安全存储，
                      含 ReportDeviceWithRisks 风险上报与 GetSecurityConfig 策略拉取，SEC-1）
```

**推荐**：统一入口 `c, _ := aohoyo.New(...)` → `c.S2S.Upload()` / `c.Stats.ReportEvent()`

**签名规则**：`HMAC-SHA256(appID + "\n" + timestamp + "\n" + body, appSecret)`，与 AS `pkg/sign` 完全同构。multipart 必须先构造 body 再签名。

## 接入方使用

```go
import "github.com/langhuachuanshi/aohoyo-sdk/go"

// BaseURL 为域名根（例如 https://api.aohoyo.com），SDK 内部拼 /as/v1/storage/*、/as/v1/stats/*
c, _ := aohoyo.New(appID, appSecret, "https://api.aohoyo.com")
c.S2S.Upload(...)
c.Stats.ReportEvent(...)
```

三要素：`AppID`（公开）、`AppSecret`（保密）、`BaseURL`（AS 地址）。
本地联调可用 `replace github.com/langhuachuanshi/aohoyo-sdk/go => /path/to/aohoyo-sdk/go`。

## 发布

**Tag 格式**：`go/v*`（本仓库 `go/` 目录，Go proxy 按 semver 解析；当前主线 v1.x）。

```bash
git tag -a go/vX.Y.Z -m "go/vX.Y.Z: ..."
git push origin go/vX.Y.Z   # 触发 CI: go vet + test
```

> 历史遗留 tag（`server-go/v*`、`sdk/server-go/v*`，来自 monorepo 拆分前）已保留不删；
> 版本号承接 `go/v1.0.0`（`go/v0.10.0` 为其后发布的降版历史遗留，semver 上 v1.1.0 > v1.0.0 > v0.10.0）。

打 tag 前四项检查（与根 AGENTS.md 发布标准一致）：
1. 版本号由 tag 决定（无版本文件）
2. `CHANGELOG.md` 定稿 `[Unreleased]` → 版本节，行为变更显式标注
3. **README 对照检查**（新特性进能力表，已下线 API 删除）
4. `go build ./... && go vet ./... && go test ./...` 全过

## 开发约定

- 零三方依赖；签名协议与 AS `pkg/sign` 保持一致
- 错误信息中文化；`sign.go` 改动必须更新 `sign_test.go`
