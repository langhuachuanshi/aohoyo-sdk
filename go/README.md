# @aohoyo/server-go

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)
![MIT License](https://img.shields.io/badge/License-MIT-green)

Aohoyo 服务端 Go SDK，给外部 Go 后端服务（如 wilas）集成。封装 AS（admin-server）对外接口的 S2S 签名调用，提供存储上传/删除等开箱即用的能力。

## 安装

```bash
go get github.com/aohoyo/sdk-go
```

> sdk-go 目前在 aohoyo 仓库内（`sdk/server-go/`），未单独发布到 GitHub 公开仓库。
> 外部项目可用 replace 指向本地路径或仓库内引用：
> ```go
> replace github.com/aohoyo/sdk-go => /path/to/aohoyo/sdk/server-go
> ```

## 模块

| 包 | 作用 | 鉴权 |
|----|------|------|
| 根包 `aohoyo` | **统一入口**（推荐）：一次初始化拿全部能力 | — |
| [`s2s`](./s2s/) | S2S 签名 + 对外存储接口客户端（Upload / GetUploadToken / Delete） | app_secret 签名 |
| [`stats`](./stats/) | 统计事件上报（ReportEvents / ReportEvent） | 公开接口，无需签名 |

## 快速开始（统一入口，推荐）

一次初始化，所有能力挂在 `c.S2S` / `c.Stats` 上：

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/aohoyo/sdk-go"       // 统一入口
	"github.com/aohoyo/sdk-go/stats" // 事件类型常量
)

func main() {
	c, err := aohoyo.New(
		os.Getenv("AOHOYO_APP_ID"),
		os.Getenv("AOHOYO_APP_SECRET"),
		"https://api.aohoyo.com",
	)
	if err != nil { panic(err) }

	data, _ := os.ReadFile("./test.zip")

	// ① 存储操作（自动 S2S 签名）
	res, err := c.S2S.Upload(context.Background(), "wilas/files/", "test.zip", data)
	if err != nil { panic(err) }
	fmt.Println("上传成功:", res.URL)

	// ② 统计上报（公开接口）
	err = c.Stats.ReportEvent(context.Background(), stats.Event{
		EventType: stats.EventCustom,
		SessionID: "s1",
		Platform:  stats.PlatformWindows,
		Title:     "文件下载",
	})
	if err != nil { panic(err) }
}
```

### 也可以只用子包（向后兼容）

如果只用到单一能力，可直接用子包：

```go
import "github.com/aohoyo/sdk-go/s2s"

c := s2s.New(appID, appSecret, baseURL)
c.Upload(ctx, "files/", "test.zip", data)
```

## 签名协议

与 AS 的 DeviceSign 同构（`deviceID` 换成 `appID`），详见 [S2S 签名 Spec](../../docs/specs/s2s-sign.md)。

```
signData  = appID + "\n" + timestamp + "\n" + body
signature = HMAC-SHA256(appSecret, signData)  → hex

请求头：
  X-App-ID:        APP_xxx
  X-S2S-Timestamp: <秒级时间戳>
  X-S2S-Signature: <hex>
```

## License

MIT
