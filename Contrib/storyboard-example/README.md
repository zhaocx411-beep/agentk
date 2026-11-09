# storyboard-example

这是一个跨语言、独立部署的 agent-k Capability 示例。Go 和 Python
实现注册相同的 Temporal Workflow Type、遵循相同的 JSON 契约，但使用不同的
Capability ID，并分别监听独立的 Task Queue：

| 实现 | Capability ID | Workflow Type | Task Queue |
|---|---|---|---|
| Go | `contrib.storyboard_example.storyboard_film_go` | `StoryboardFilmExample` | `contrib.storyboard-example-go` |
| Python | `contrib.storyboard_example.storyboard_film_python` | `StoryboardFilmExample` | `contrib.storyboard-example-py` |

Workflow 先使用大语言模型生成分镜，再并发生成各镜头的图片和视频，最后拼接成功生成的
视频片段。完整 ABI 见 [CONTRACT.md](CONTRACT.md)。

## 如何选择子目录

每个子目录都是一个可独立运行和部署的示例，不是同一个应用里的分层 package：

| 子目录 | 进程角色 | 适合参考的场景 |
|---|---|---|
| [`go/worker`](go/worker) | Go Temporal Worker | 用 Go 为 agent-k 扩展 Workflow Capability，并声明 Tool 暴露策略 |
| [`go/apiserver`](go/apiserver) | Go HTTP API Server | 下游 Go 应用通过 AgentK API 创建 Session、启动 Pipeline，并包装成自己的异步 Job API |
| [`java/apiserver`](java/apiserver) | Java HTTP API Server | 下游 Java 应用通过 AgentK API 复用已部署的 Go Capability，并包装成异步 Job API |
| [`python/worker`](python/worker) | Python Temporal Worker | 用 Python 实现与 Go Worker 相同 ABI 的 Workflow Capability |
| [`python/mcp-server`](python/mcp-server) | Python MCP Server | 下游应用把 AgentK Pipeline 包装成 MCP Tool |

如果要贡献新的能力实现，参考对应语言的 `worker`；如果能力已经部署，只想从下游应用
接入 AgentK，HTTP 服务参考 `go/apiserver`，MCP 服务参考 `python/mcp-server`。两个
Worker 是同一能力 ABI 的替代实现，分别监听独立 Task Queue；两个 Server 则是不同的
下游消费方式，各自使用相应 Worker 的 Profile。

## 重要提示

运行本示例会实际调用文本、图片和视频生成能力，可能需要数分钟并产生模型调用费用。
首次验证请使用 `shots: 1`。

本仓库的 Worker 通过 AgentK Worker SDK 0.2.2 发布 Capability Manifest，并将 Workflow
暴露为 Tool。Capability ID 是能力的唯一身份；Pipeline 名称由 Session
Profile 绑定，不在 Worker 发布声明中重复定义。除此之外还提供三种独立的下游应用示例：

- `go/apiserver` 通过 AgentK Go Client 的 KESS gRPC transport 调用 Pipeline，对外暴露异步 Storyboard Job REST API；
- `java/apiserver` 通过 AgentK Java Client 的 KESS gRPC transport 调用同一 Go Pipeline，对外暴露相同的 Job REST API；
- `python/mcp-server` 通过 AgentK Python Client 的 KESS gRPC transport 调用 Pipeline，并向 MCP Client 暴露一个
  `generate_storyboard_film` Tool。

## 前置条件

向 agent-k 维护者获取：

- agent-k staging 的访问权限；API 调用还需要相应凭证。

外部 Worker 通过本机或部署环境中的 Kess Agent 动态发现 AgentK Temporal 集群，
并固定使用 `agentk` Namespace。不要连接本机 Temporal。Worker 会将 agent-k
原子能力的 Activity 路由到核心 `agentk` Task Queue。Go Job API 和 Python MCP
Server 和 Java Job API 同样通过 KESS 自动发现 AgentK API，不需要配置固定 URL。

## 启动 Go Worker

### 配置私有 Go Module

本示例正式依赖 AgentK Go Worker SDK。该 SDK 位于公司私有 Git 仓库，协作者的机器
如果只配置了公司 `GOPROXY`，可能出现以下情况：

- `no secure protocol found for repository`；
- 下载停在 `goproxy.corp.kuaishou.com/.../agentk-sdk.git/go`。

`GONOSUMDB` 只跳过校验和数据库，不会绕过 Go Module 代理。请配置
`GOPRIVATE`；如果公司 Git 的 HTTPS 凭证不可用、但 SSH 可以访问，还需要将 HTTPS
仓库地址改写为 SSH：

```bash
go env -w \
  GOPRIVATE=git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git

git config --global \
  url."git@git.corp.kuaishou.com:".insteadOf \
  "https://git.corp.kuaishou.com/"
```

如果 `GOPRIVATE` 已有其他规则，请用逗号追加上述 SDK 仓库路径，不要覆盖原值。
这里只绕过 AgentK Worker SDK 的代理，其他已由公司 `GOPROXY` 正常提供的内部 Module
仍可继续走代理。

可以用以下命令分别确认配置与仓库权限：

```bash
go env GOPRIVATE GONOPROXY GONOSUMDB GOPROXY
git ls-remote \
  git@git.corp.kuaishou.com:klingai-agentk/agentk-sdk.git HEAD
```

### 运行

```bash
cd go/worker
go run .
```

Go 实现使用正式的 AgentK Go Worker SDK 发布 Capability、传播执行元数据并上报
Pipeline 进度。Worker 成功连接 Temporal 并开始监听后，日志中会显示
`contrib.storyboard-example-go` Task Queue。

## 启动 Go Job API

Go Job API 是一个独立的下游应用。它为每个 Job 创建一个 AgentK Session、启动
`storyboard_film_example` Pipeline，并在内存中维护业务 Job ID 到 AgentK Session
和 Pipeline Run 的映射。

先确认 Go Worker 已连接开发集群。本地连接 staging 时设置应用 KSN，SDK 会自动传播
KWS 身份：

```bash
cd go/apiserver

export KWS_SERVICE_NAME='klingai-agentk-storyboard-example-api'
export API_HOST='127.0.0.1'
export API_PORT='8080'

go run .
```

API 默认从当前目录读取 `profile.json`，并默认使用
`deepseek-v4-pro`、`z-image-turbo`、`kling-3.0`。可通过
`PROFILE_FILE`、`STORYBOARD_CHAT_MODEL`、`STORYBOARD_IMAGE_MODEL`、
`STORYBOARD_VIDEO_MODEL` 覆盖。

创建 Job：

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/jobs \
  -H 'Content-Type: application/json' \
  -d '{"topic":"雨夜里走过霓虹街道的猫","shots":1}'
```

服务会立即返回 `202 Accepted`：

```json
{
  "id": "job_...",
  "status": "running",
  "topic": "雨夜里走过霓虹街道的猫",
  "shots": 1,
  "createdAt": "..."
}
```

保存 Job ID 后查询单个 Job，或列出本进程创建的全部 Job：

```bash
export JOB_ID='job_...'
curl -sS "http://127.0.0.1:8080/v1/jobs/$JOB_ID"
curl -sS http://127.0.0.1:8080/v1/jobs
curl -sS http://127.0.0.1:8080/healthz
```

成功终态包含：

```json
{
  "id": "job_...",
  "status": "completed",
  "topic": "雨夜里走过霓虹街道的猫",
  "shots": 1,
  "output": {
    "artifacts": [
      {
        "type": "video",
        "uri": "https://..."
      }
    ],
    "clipCount": 1
  },
  "createdAt": "...",
  "finishedAt": "..."
}
```

> **样例限制：** Job 索引仅保存在当前进程内存中，服务重启后丢失；当前不提供
> Update 或取消接口，也没有后台轮询，Job 状态在 GET 时从 AgentK 刷新。第一版
> Endpoint 没有入站鉴权，只能部署在可信内网。部署环境的 KWS 身份由平台注入。

从仓库根目录分别构建 Go Worker 和 Job API 镜像：

```bash
podman build --platform linux/amd64 --ssh default \
  -f go/worker/Containerfile \
  -t storyboard-example-go-worker:dev \
  .

podman build --platform linux/amd64 \
  -f go/apiserver/Containerfile \
  -t storyboard-example-go-apiserver:dev \
  .
```

公司容器云部署使用各子目录中的 `Dockerfile`，构建上下文和平台设置见下方
[kcsize 镜像构建示例](#kcsize-镜像构建示例)。

Job API 镜像可以这样启动：

```bash
podman run --rm -p 8080:8080 \
  storyboard-example-go-apiserver:dev
```

## 启动 Java Job API

Java Job API 是 Go Job API 的纯 Java 下游版本，使用 JDK 17 和
`agentk-client-grpc:0.3.0`。它不启动 Java Worker，实际的
`storyboard_film_example` Pipeline 仍由已部署的 Go Worker 执行。

先确认 Go Worker 已连接开发集群，然后构建并启动：

```bash
cd java/apiserver

mvn clean package

export KWS_SERVICE_NAME='klingai-agentk-storyboard-example-api'
java -cp 'target/storyboard-example-java-apiserver-0.1.0-SNAPSHOT.jar:target/lib/*' \
  com.kuaishou.klingai.agentk.storyboard.apiserver.Main
```

默认监听 `127.0.0.1:8080`，并从当前目录读取 `profile.json`。
配置项、Job REST 契约和 curl 用法与前文 Go Job API 相同；API 文档位于
`http://127.0.0.1:8080/docs`。

从仓库根目录构建本地镜像：

```bash
podman build --platform linux/amd64 \
  -f java/apiserver/Containerfile \
  -t storyboard-example-java-apiserver:dev \
  .

podman run --rm -p 8080:8080 \
  storyboard-example-java-apiserver:dev
```

## 准备 Python workspace

Python Worker 和 MCP Server 使用同一个 uv workspace 与虚拟环境。本地同时运行两个
进程前，先从 `python/` 一次性安装所有成员及测试依赖：

```bash
cd python
uv sync --all-packages --all-extras
```

后续分别使用 `uv run --no-sync` 启动，避免启动第二个成员时重新裁剪共享虚拟环境。

## 启动 Python Worker

```bash
cd python/worker

uv run --no-sync python worker.py
```

Worker 成功连接 Temporal 后会打印：

```text
worker started: task queue "contrib.storyboard-example-py", workflow "StoryboardFilmExample"
```

Python 实现使用正式的 AgentK Python Worker SDK 发布同一份 Capability 契约，并上报
Pipeline 进度。

## 启动 Python MCP Server

Python MCP Server 是独立的下游应用入口。它通过 AgentK API 创建 Session、启动
`storyboard_film_example` Pipeline，并阻塞等待最终视频。等待期间会通过 MCP
Progress Notification 上报 Session 创建、Pipeline 启动和已等待时间。支持 MCP
Apps 的 Host 会在调用完成后内嵌视频播放器；不支持 Apps 的 Client 仍会收到完整的
文本和结构化结果。

MCP App 是一个无需前端构建或外部运行时依赖的原生 HTML/CSS/JavaScript 单文件。
确认 Python Worker 已连接开发集群后，配置应用身份并启动 Streamable HTTP Server：

```bash
cd python/mcp-server

export KWS_SERVICE_NAME='klingai-agentk-storyboard-example-api'
export MCP_HOST='0.0.0.0'
export MCP_PORT='8000'
export MCP_ALLOWED_HOST='localhost' # 部署时改为实际内网域名

uv run --no-sync python main.py
```

MCP Endpoint 为 `http://<host>:8000/mcp`，健康检查为：

```bash
curl -sS http://localhost:8000/healthz
```

Tool 只要求 `topic`，可选 `shots` 默认为 `1`、范围为 1–8。模型默认使用本文 Args
示例中的三个模型，也可分别通过 `STORYBOARD_CHAT_MODEL`、
`STORYBOARD_IMAGE_MODEL`、`STORYBOARD_VIDEO_MODEL` 覆盖。

可以运行 MCP Inspector 验证内嵌界面：

```bash
npx @modelcontextprotocol/inspector
```

在 Inspector 中添加 `http://localhost:8000/mcp`，选择 HTTP Transport，调用
`generate_storyboard_film` 后在 Apps 视图查看播放器。`ui://storyboard-film/player.html`
是由 MCP Host 渲染的资源，不是可以直接在浏览器地址栏访问的网页。

> **安全提示：** 第一版 MCP Endpoint 没有入站鉴权，只能部署在可信内网。部署环境的
> KWS 身份由平台注入。MCP Client
> 是否展示 Progress Notification 取决于客户端实现。

从仓库根目录构建并运行容器：

```bash
podman build \
  -f python/mcp-server/Containerfile \
  -t storyboard-example-mcp:dev \
  .

podman run --rm -p 8000:8000 \
  -e MCP_ALLOWED_HOST='localhost' \
  storyboard-example-mcp:dev
```

## kcsize 镜像构建示例

Go 和 Python 的 `Dockerfile` 演示基于 kcsize 运行时构建业务镜像，本示例不实际部署到容器云。
现有 `Containerfile` 继续用于本地运行。
下列路径均相对于 **storyboard-example 仓库根目录**：

| 服务 | Dockerfile | 构建上下文 |
|---|---|---|
| Go Job API | `go/apiserver/Dockerfile` | `go/apiserver` |
| Go Worker | `go/worker/Dockerfile` | `go/worker` |
| Python MCP Server | `python/mcp-server/Dockerfile` | `.` |
| Python Worker | `python/worker/Dockerfile` | `.` |

Go 使用 `golang:1.26.7-alpine` 构建，运行在
`registry.corp.kuaishou.com/ksbase/os/os/kcsize:1.0.0-alpine3.24.1-r1`。
Python 使用 `registry.corp.kuaishou.com/ksbase/build/python/python:3.13.15` 构建，运行在
`registry.corp.kuaishou.com/ksbase/runtime/python/kcsize:3.13.15-kcsize1.0.0-trixie-slim-r1`。
两个 Python 构建都读取共享的 `python/uv.lock`，分别安装各自 workspace package；
`libsnappy-dev` 只在构建阶段安装，运行镜像已经带有 `libsnappy1v5`。

四个镜像都以 kcsize 作为容器入口，通过 `SUPERVISOR_PROGRAM0` /
`SUPERVISOR_COMMAND0` 托管业务进程。平台不要用 `python`、业务二进制或 `sleep`
覆盖容器入口；也不要覆盖 Dockerfile 中已有的 supervisor 业务命令。
应用的 `KWS_SERVICE_NAME` 身份由平台注入。

当前通用 Alpine kcsize 镜像仍继承 Alpine 的 `/bin/sh` 默认命令，因此 Go 的两个
Dockerfile 显式设置 `CMD ["/usr/local/bin/kcsize"]`；Python 运行镜像已包含该默认命令。

### HTTP 服务：Go Job API、Python MCP Server

启动脚本将平台分配的 `AUTO_PORT0` 转为 `API_PORT` 或 `MCP_PORT`；缺少该变量时直接失败。
两个服务的 `/healthz` 都返回纯文本 `ok`。以下参数在平台配置，Dockerfile 不固化：

```text
SERVER_TYPE=http
PREHEAT_URL=/healthz
PREHEAT_RESPONESE=ok
HEALTH_CHECK_URL=/healthz
HEALTH_CHECK_RESPONSE=ok
```

`PREHEAT_RESPONESE` 按平台字段的实际拼写填写。MCP 另需将 `MCP_ALLOWED_HOST`
配置为客户端使用的实际内网域名；两种 HTTP 服务仍只允许部署在可信内网。

### 非 HTTP 服务：Go Worker、Python Worker

Worker 保持原有启动和退出流程，由 supervisor 直接启动业务进程。
本示例不接入平台日志就绪检查，也不额外启动 HTTP 健康服务。

### 办公网本地构建验证

本地拉取离线仓库地址并打上线上名称，构建时使用 `--pull=never`；提交平台的 Dockerfile
始终保持线上域名和 `ksbase` 命名空间。以下命令只构建镜像，不尝试在本地启动 kcsize：

```bash
export KWS_SERVICE_NAME=klingai-agentk-storyboard-example-api

for image in \
  build/golang/golang:1.26.7-alpine \
  os/os/kcsize:1.0.0-alpine3.24.1-r1 \
  build/python/python:3.13.15 \
  runtime/python/kcsize:3.13.15-kcsize1.0.0-trixie-slim-r1
do
  podman pull --platform linux/amd64 --tls-verify=false \
    "registry-internal.corp.kuaishou.com/ksbase-test/${image}"
  podman tag \
    "registry-internal.corp.kuaishou.com/ksbase-test/${image}" \
    "registry.corp.kuaishou.com/ksbase/${image}"
done

podman build --platform linux/amd64 --pull=never \
  -f go/apiserver/Dockerfile -t storyboard-example-go-apiserver:kcsize go/apiserver
podman build --platform linux/amd64 --pull=never \
  -f go/worker/Dockerfile -t storyboard-example-go-worker:kcsize go/worker
podman build --platform linux/amd64 --pull=never \
  -f python/mcp-server/Dockerfile -t storyboard-example-mcp:kcsize .
podman build --platform linux/amd64 --pull=never \
  -f python/worker/Dockerfile -t storyboard-example-python-worker:kcsize .
```

## 通过 AgentK staging 发起请求

Worker、Go/Java Job API 和 Python MCP Server 都通过 KESS 连接已部署的 AgentK，不需要
在本机启动 Temporal 或 AgentK，也不使用旧 REST gateway。本地 staging 设置
`KWS_SERVICE_NAME=klingai-agentk-storyboard-example-api` 后，可以选择前文任一入口完成端到端验证：

- 启动 Go Worker 和 Go Job API，通过 `POST /v1/jobs` 发起 Go Pipeline；
- 启动 Go Worker 和 Java Job API，通过同一 `POST /v1/jobs` 发起 Go Pipeline；
- 启动 Python Worker 和 Python MCP Server，通过 `generate_storyboard_film` 发起 Python Pipeline。

三条路径使用相同的 Pipeline 名称和参数，区别仅在 Profile 引用的 Capability ID。

## 测试

```bash
(cd go/worker && go test -race ./...)
(cd go/apiserver && go test -race ./...)
(mvn -f java/apiserver/pom.xml clean verify)
(cd python && uv sync --locked --all-packages --all-extras)
(cd python/worker && uv run --locked --project . --extra test pytest tests)
(cd python/mcp-server && uv run --locked --project . --extra test pytest tests)
```

两个实现有意使用不同的 Task Queue。Temporal 会先按 Task Queue 路由，再按
Workflow Type 查找实现；共享同一 Task Queue 的 Worker 必须注册完全相同的
Workflow 和 Activity Type 集合。
