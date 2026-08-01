# aohoyo-sdk

aohoyo 平台 SDK 集合。给外部项目接入 aohoyo 用户系统与应用管理。

## SDK 列表

| SDK | 目录 | 语言 | 消费者 | 发布 |
|-----|------|------|--------|------|
| server-go | `go/` | Go | 外部后端（wilas 等） | `go get github.com/langhuachuanshi/aohoyo-sdk/go` |
| client-js | `js/` | TypeScript | 外部前端（浏览器/APP） | `npm install @aohoyo/client-sdk` |

## 快速开始

### Go SDK

```go
import "github.com/langhuachuanshi/aohoyo-sdk/go"

c, _ := aohoyo.New(appID, appSecret, "https://api.aohoyo.com")
// 存储上传
res, _ := c.S2S.Upload(ctx, "files/", "test.zip", data)
// 统计上报
c.Stats.ReportEvent(ctx, stats.Event{...})
```

### JS SDK

```ts
import { createSdk } from '@aohoyo/client-sdk'

const sdk = createSdk({ baseURL: 'https://api.aohoyo.com', app_id: '...' })
await sdk.auth.login({ username: 'test', password: '123456' })
```

## 发布

| SDK | Tag | CI |
|-----|-----|-----|
| server-go | `go/v*` | go vet + test |
| client-js | `js/v*` | npm publish |

## 契约

平台契约文档在[主仓库](https://github.com/langhuachuanshi/aohoyo) `docs/specs/`。
本仓库只维护 SDK 的 CHANGELOG(API 变更)和各子包 README(API 参考)。

## License

MIT
