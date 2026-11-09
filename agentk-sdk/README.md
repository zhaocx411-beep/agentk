# AgentK SDK

AgentK 的多语言 SDK：API 客户端、Temporal Worker 能力发布。

| 目录 | 内容 |
|---|---|
| `go/` | Go 客户端与 Worker SDK |
| `python/` | Python 客户端与 Worker SDK，见 [`python/README.md`](python/README.md) |
| `java/` | Java 客户端 |
| `protocol/` | 跨语言协议定义与契约文档 |

请求泳道由 ksresolve 的 trace carrier（`trace_context-bin`）承载，SDK 不再定义
自己的泳道 header。Worker 实例泳道的跨语言契约见
[`protocol/agentk/worker_lane/v1`](protocol/agentk/worker_lane/v1/README.md)。

## Go

```bash
go get git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go@latest
```

```go
import (
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/discovery"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
)
```

Go module 位于仓库子目录 `go/`，发布版本使用 `go/vX.Y.Z` 格式的 Git tag。

在 `go/` 目录运行验证：

```bash
go test -race ./...
go vet ./...
```
