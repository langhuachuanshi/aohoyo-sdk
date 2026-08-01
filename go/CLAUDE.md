# sdk/server-go 开发规范

## 项目定位

服务端 Go SDK，给外部 Go 后端（wilas 等）集成。用 S2S 签名调 AS 对外接口（`/api/storage/*`）。

与 [`sdk/client-js`](../client-js/CLAUDE.md) 区别：client-js 是客户端 SDK（JWT），server-go 是服务端 SDK（S2S 签名）。

S2S 协议见 [`docs/specs/s2s-sign.md`](../../docs/specs/s2s-sign.md)。

## 技术栈

Go 1.21+，**零三方依赖**（仅标准库），module：`github.com/aohoyo/sdk-go`

## 架构

```
├── client.go       ← 统一入口 aohoyo.New(appID, secret, baseURL)，组合 s2s + stats
├── s2s/            ← S2S 签名 + 存储客户端（Upload/GetUploadToken/Delete）
└── stats/          ← 统计上报（公开接口，无需签名）
```

**推荐**：统一入口 `c, _ := aohoyo.New(...)` → `c.S2S.Upload()` / `c.Stats.ReportEvent()`

**签名规则**：`HMAC-SHA256(appID + "\n" + timestamp + "\n" + body, appSecret)`，与 AS `pkg/sign` 完全同构。multipart 必须先构造 body 再签名。

## 接入方使用

```go
import "github.com/aohoyo/sdk-go"

// BaseURL 指向 /api 路由组根（例如 https://api.aohoyo.com），SDK 内部拼 /v1/storage/*、/v1/stats/*
c, _ := aohoyo.New(appID, appSecret, "https://api.aohoyo.com")
c.S2S.Upload(...)
c.Stats.ReportEvent(...)
```

外部项目用 replace：
```go
replace github.com/aohoyo/sdk-go => /path/to/aohoyo/sdk/server-go
```

三要素：`AppID`（公开）、`AppSecret`（保密）、`BaseURL`（AS 地址）。

## 发布

**Tag 格式**：`sdk/server-go/v*`（module = `github.com/langhuachuanshi/aohoyo/sdk/server-go`，tag 必须带目录前缀）。

```bash
git tag -a sdk/server-go/vX.Y.Z -m "sdk/server-go: ..."
git push origin sdk/server-go/vX.Y.Z
```

> 历史遗留 `server-go/v0.2.0`~`v0.8.0`（漏 `sdk/`）已保留不删，下游统一用 `sdk/server-go/v*`。计划迁到独立仓库 `aohoyo-sdk` 后改为 `go/v*`（见 `docs/plans/sdk-monorepo-split.md`）。

打 tag 前必须：`go build ./... && go test ./... && go vet ./...` 全过 + `CHANGELOG.md` 更新。目前无自动发布。

## 开发约定

- 零三方依赖；签名协议与 AS `pkg/sign` 保持一致
- 错误信息中文化；`sign.go` 改动必须更新 `sign_test.go`
