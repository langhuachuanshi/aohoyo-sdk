# aohoyo-sdk 开发规范

aohoyo 平台 SDK 统一仓库。Go SDK 在 `go/`，JS SDK 在 `js/`，未来新增 SDK（Python/Flutter 等）直接加目录。

平台契约见[主仓库](https://github.com/langhuachuanshi/aohoyo) `docs/specs/`。

## 分支与发布

| 分支 | 用途 |
|------|------|
| `main` | 主干，日常开发 + 发布归档 |

**Tag 触发发布：**

| SDK | Tag | 触发 |
|-----|-----|------|
| server-go | `go/v*` | Go proxy 自动解析 + CI lint/test |
| client-js | `js/v*` | CI → npm publish |

Go module 路径：`github.com/langhuachuanshi/aohoyo-sdk/go`
npm 包名：`@aohoyo/client-sdk`

**打 tag 前**：更新对应 SDK 的 `CHANGELOG.md` + `go test ./...` / `npm run build` 通过。

## 目录结构

```
aohoyo-sdk/
├── go/                    ← server-go SDK
│   ├── go.mod             # module github.com/langhuachuanshi/aohoyo-sdk/go
│   ├── client.go          # 统一入口 aohoyo.New()
│   ├── s2s/               # S2S 签名 + 存储客户端
│   └── stats/             # 统计上报
├── js/                    ← client-js SDK
│   ├── package.json       # @aohoyo/client-sdk
│   └── src/
├── .github/workflows/
│   ├── ci-go.yml          # go/v* → go vet + test
│   └── ci-js.yml          # js/v* → npm publish
├── README.md
└── CLAUDE.md              ← 你在这里
```

## API 版本

接口使用服务前缀 + 版本号：UC 接口 `/uc/v1/*`，AS 接口 `/as/v1/*`（主仓库 2026-08-10 路由架构重构）。
SDK 的 `baseURL` 不需要带 `/api`，直接用域名或空字符串（相对路径）。

## 新增 SDK

在新目录下建项目（如 `python/`、`flutter/`），tag 用 `python/v*`、`flutter/v*`。根 README 更新 SDK 列表。
