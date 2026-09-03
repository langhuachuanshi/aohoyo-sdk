# aohoyo-sdk 开发规范

aohoyo 平台 SDK 统一仓库。Go SDK 在 `go/`，JS SDK 在 `js/`，Rust 原生模块在 `rust/native/`，未来新增 SDK（Python/Flutter 等）直接加目录。

平台契约见[主仓库](https://github.com/langhuachuanshi/aohoyo) `docs/specs/`。

> 本文件是各 Agent 工具（ZCode / Claude Code / Codex 等）的统一入口规范；
> 各子目录的 `AGENTS.md` 是该 SDK 的知识库。`CLAUDE.md` 仅作指针保留（多 Agent 兼容）。

## 分支与发布

| 分支 | 用途 |
|------|------|
| `main` | 主干，日常开发 + 发布归档 |

**Tag 触发发布：**

| SDK | Tag | 触发 |
|-----|-----|------|
| server-go | `go/v*` | Go proxy 自动解析 + CI vet/test |
| client-js | `js/v*` | CI → npm publish（推送即发布，注意部署节奏） |
| desktop-native-rust | `rust/v*` | CI cargo test（ubuntu + windows 矩阵） |

Go module 路径：`github.com/langhuachuanshi/aohoyo-sdk/go`
npm 包名：`@aohoyo/client-sdk`
Rust crate：`aohoyo-native`

> 历史遗留 tag（`server-go/v*`、`sdk/server-go/v*`）已保留不删；go 版本号承接 `go/v1.0.0`。

## 发布标准（打 tag 前四项检查，缺一不打）

1. **版本号 bump**：js 改 `package.json`；go/rust 版本由 tag 决定（无版本文件）。
2. **CHANGELOG**：对应 SDK 的 `CHANGELOG.md` 定稿 `[Unreleased]` → 版本节，行为变更显式标注。
3. **README 对照检查**：本 SDK 新特性是否已进 README 能力表/快速开始，已下线 API 是否已删除——README 管「当前面」，CHANGELOG 管「历史」，每次发布做增量更新。
4. **构建测试绿**：js `npm run build`（tsc strict）；go `go build ./... && go vet ./... && go test ./...`；rust `cargo test`。

## 目录结构

```
aohoyo-sdk/
├── go/                    ← server-go SDK + desktop-native-go（go/native/）
│   ├── go.mod             # module github.com/langhuachuanshi/aohoyo-sdk/go
│   ├── client.go          # 统一入口 aohoyo.New()，组合 s2s + stats + uc
│   ├── s2s/               # S2S 签名 + 存储客户端
│   ├── stats/             # 统计上报
│   ├── uc/                # 用户中心 Token 验证（Bearer 透传）
│   └── native/            # 桌面端原生安全模块（Wails 可绑定，零三方依赖）
├── js/                    ← client-js SDK
│   ├── package.json       # @aohoyo/client-sdk
│   └── src/
├── rust/native/           ← desktop-native-rust（Tauri 可绑定）
├── .github/workflows/
│   ├── ci-go.yml          # go/v* → go vet + test
│   ├── ci-js.yml          # js/v* → npm publish
│   └── ci-rust.yml        # rust/v* → cargo test 矩阵
├── README.md
└── AGENTS.md              ← 你在这里
```

## API 版本

接口使用服务前缀 + 版本号：UC 接口 `/uc/v1/*`，AS 接口 `/as/v1/*`（主仓库 2026-08-10 路由架构重构）。
SDK 的 `baseURL` 不需要带 `/api`，直接用域名或空字符串（相对路径）。

## 新增 SDK

在新目录下建项目（如 `python/`、`flutter/`），tag 用 `python/v*`、`flutter/v*`。根 README 更新 SDK 列表，新目录建自己的 `AGENTS.md`。
