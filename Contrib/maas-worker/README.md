# MaaS AgentK Adapter

本仓库构建同一个镜像中的两个独立程序，但线上必须部署为两个单元：

```text
AgentK Staging Temporal
        ↓ Activity
maas-worker（Staging，仅 AgentK SDK Worker）
        ↓ HTTPS / KNGX
maas-gateway（PRT，仅 HTTP → KESS RPC）
        ↓ KESS RPC
PRT MaaS 服务
```

- `maas-gateway` 不依赖 AgentK SDK 的运行时行为，不连接 Temporal；
- `maas-worker` 不创建 KESS RPC client，不加载 PRT serviceName 或 lane；
- `process_video`、`compose_video` 和 `extract_audio` 也只通过 HTTPS 调用 `maas-gateway` 的
  `ffmpeg` route，Worker 镜像不安装或执行 `ffmpeg`；
- 两边配置分别使用 `routes` 和 `capabilities` 文档，避免把 PRT RPC 配置带到 Staging；
- Worker 只接受 HTTPS Gateway URL。开发机必须显式设置
  `MAAS_GATEWAY_ALLOW_INSECURE_HTTP=on` 才能使用 HTTP。

## 配置边界

PRT Gateway 使用 `configs/routes.example.json`：

```json
{
  "routes": {
    "transnet": {
      "serviceName": "maas-agentalgo-klingai-transnet-v2-pytorch",
      "timeoutSeconds": 600,
      "allowedFeatures": ["video", "min_shot_duration"]
    }
  }
}
```

Staging Worker 使用 `configs/capabilities.example.json`：

```json
{
  "capabilities": {
    "transnet": {
      "timeoutSeconds": 600,
      "agentk": {
        "id": "contrib.maas.transnet",
        "description": "使用 TransNet 检测视频中的镜头边界。",
        "toolDescription": "需要分析镜头边界时使用。",
        "maxAttempts": 1,
        "inputSchema": {"type": "object", "required": ["video"]},
        "outputSchema": {"type": "object"}
      }
    }
  }
}
```

默认情况下，capability map key 同时是 Gateway route（示例为 `transnet`）。需要让多个
AgentK capability 复用同一 Gateway route 时，使用独立的 `gatewayRoute`；Activity type
仍只由 capability map key 决定，例如 `compose-video` 始终对应
`maas.infer.compose_video.v1`，调整后端 route 不会改变 Temporal ABI。

生产环境建议把两份完整 JSON 分别放在 KConf：

- PRT Gateway 设置 `MAAS_ROUTES_KCONF_KEY`；
- Staging Worker 设置 `MAAS_CAPABILITIES_KCONF_KEY`。

配置来源优先级为 KConf key、JSON 环境变量、文件。只要设置了 KConf key，拉取失败、
空值或 JSON 校验失败都会拒绝启动，不会静默使用旧的环境变量或文件。当前 KConf 在进程
启动时读取；修改后通过滚动重启 Pod 生效。Gateway 的 KESS client 和 Worker 的
Temporal Activity 注册都不做不安全的进程内热替换。

## 部署

镜像同时包含 `/home/web_server/maas-worker/bin/maas-gateway` 和
`/home/web_server/maas-worker/bin/maas-worker`。启动脚本要求显式设置
`MAAS_ROLE=gateway|worker`，不会默认把二者启动在同一进程或同一 Pod。

### 1. PRT：Gateway 部署单元

必须设置：

- `MAAS_ROLE=gateway`
- `MAAS_ROUTES_KCONF_KEY`（生产推荐），或 `MAAS_ROUTES_JSON`，或挂载文件并设置
  `MAAS_ROUTES_FILE`
- `KWS_SERVICE_NAME` 及 PRT KESS RPC 所需的平台身份变量
- `AUTO_PORT0`（kcsize 下发）

可选设置：

- `MAAS_GATEWAY_BEARER_TOKEN`：可选的联调 Bearer 校验；内部可信 KNGX 部署不设置；
- `MAAS_MAX_REQUEST_BYTES`：默认 16 MiB，最大 128 MiB；
- `MAAS_SHUTDOWN_TIMEOUT`：默认 `30s`；
- `MAAS_HTTP_ADDR`：非 kcsize 本地运行时使用，默认 `:8080`。

该部署单元配置 HTTP `/healthz` 探活，并由内部 KNGX 暴露。按当前平台约定，内部
KNGX 默认可信，不要求额外认证。

### 2. Staging：AgentK Worker 部署单元

必须设置：

- `MAAS_ROLE=worker`
- `MAAS_CAPABILITIES_KCONF_KEY`（生产推荐），或 `MAAS_CAPABILITIES_JSON`，或挂载
  文件并设置 `MAAS_CAPABILITIES_FILE`
- `MAAS_GATEWAY_BASE_URL=https://<KNGX host>/<optional-prefix>`
- `KWS_SERVICE_NAME`：Staging 的 Worker 服务身份，`sdktemporal.Dial()` 使用它连接
  Staging AgentK Temporal

可选设置：

- `MAAS_AGENTK_TASK_QUEUE`：默认 `${KWS_SERVICE_NAME}-agentk`；所有副本必须一致；
- `MAAS_GATEWAY_BEARER_TOKEN`：仅在联调时显式启用 Bearer；默认不设置；
- `MAAS_GATEWAY_ALLOW_INSECURE_HTTP=on`：只允许本地测试使用。

Worker 在 `AUTO_PORT0` 只暴露 `GET /healthz` 和 `GET /readyz`，不注册 `/v1/routes`
或 `/v1/infer/*`，因此不会变成 Gateway。两个部署单元都使用 HTTP `/healthz` 探活。

Session Profile 仍需显式引用 capability，参考
`configs/agentk-profile.example.json`；发布 capability 不会修改已有 Session Profile。

### FFmpeg capability 适配器

`agentk.process_video`、`agentk.compose_video` 和 `agentk.extract_audio` 原先是 AgentK core capability。外部
Worker 的发布协议只允许 `contrib.<owner>.<name>`，因此迁入本仓库后使用：

- `contrib.maas.process_video`（Profile 工具名仍可设为 `process_video`）；
- `contrib.maas.compose_video`（Profile 工具名仍可设为 `compose_video`）；
- `contrib.maas.extract_audio`（Profile 工具名仍可设为 `extract_audio`）。

capabilities 文档用 `gatewayRoute: "ffmpeg"` 把三个 Activity 复用到同一个 Gateway
route，再用 `adapter: "process_video"|"compose_video"|"extract_audio"` 做 AgentK 契约与受控 FFmpeg
`pipeline` 的转换。适配器只传 JSON features 和媒体引用，不下载媒体、不执行命令。
`process_video` 与原 builtin 契约一致：在一次受控转码内按顺序执行裁剪、30fps/48kHz
规格归一化、保留原声的轻/中/强背景降噪、cover/contain/stretch 画幅调整、0.5～2
倍音视频同步变速和静音，并支持 high/balanced/small 三档输出质量。
`compose_video` 与原 builtin 契约一致：按首段显示尺寸或 1080p 档归一为 30fps
H.264/AAC MP4，居中裁切不同画幅，缺音轨自动补静音；外部音轨支持时间线偏移、
源内裁剪、时长、音量和循环，并支持 SRT/ASS 字幕。`extract_audio` 支持 MP3、
M4A/AAC、WAV、码率、采样率、声道和 loudnorm。所有处理仍在 MaaS ffmpeg 服务完成。

`result.artifacts` 是 AgentK 的标准资源结果信封，不是 Blob 上传 API。产出方必须先把
媒体写到可供后续工具读取的位置，再把 URI 放入 `artifacts[].uri`；AgentK 会把该 URI
登记到 Session 事件和 `list_artifacts`，不会接管产物字节。

Worker 把 MaaS 返回的 `db:table:key` 物理引用转换为与 endpoint、CDN 和部署环境无关的
逻辑 Blob URI：`blob://db-table/key`。如果上游已经返回 HTTP(S) URI，Worker 原样保留。
Worker 不读取任何 AgentK Artifact Storage 配置，也不包含 Blob endpoint 硬编码。

`artifactOutputs` 用来声明普通 MaaS capability 的哪些响应字段是媒体产物及其类型；
Worker 保留原始 `db:table:key` 字段供即时工具链继续使用，同时生成标准 `artifacts`。
`process_video`、`compose_video` 和 `extract_audio` 的适配器直接生成该信封。

FFmpeg 服务上传产物所需的 `db/table` 属于 PRT 部署配置，由 Gateway route 的
`defaultFeatures` 注入，例如：

```json
"defaultFeatures": {
  "db": "<agentk-artifact-db>",
  "table": "<agentk-artifact-table>"
}
```

`process_video`、`compose_video` 和 `extract_audio` 可成对接收 `db/table`，并把它们传给每个会产生媒体
输出的 MaaS ffmpeg 请求；显式值覆盖 Gateway route 默认目标。未传时由 Gateway route
的 `defaultFeatures` 注入，适合固定产物命名空间的部署。两种方式都只决定远端写入目标，
不要求 Worker 配置 Blob endpoint 或 CDN。

`blob://` 只表达 bucket/key 身份。FFmpeg 适配器接收上一步的
`blob://db-table/key` artifact URI 时，会把 bucket 按第一个 `-` 还原成 `db/table`，
再以 Films 的 `db:table:key` 物理引用传给 MaaS ffmpeg；Worker 不读取媒体字节，也不需要
Blob endpoint。为保证该转换无歧义，FFmpeg 产物命名空间的 `db` 不应包含 `-`（`table`
可以包含）。普通 capability 的原始 `db:table:key` 响应字段仍会保留。

Gateway 的 `ffmpeg` route 必须允许 `refs` feature。`blob://db-table/key`、
`db:table:key` 和 HTTP(S) URL 都可作为三个适配器的输入；Worker 规范化后由远端 FFmpeg
服务读取，返回的媒体引用再包装为 AgentK artifact。

## HTTP 契约与幂等

Gateway 只暴露窄 route：

```http
POST /v1/infer/{route}
Content-Type: application/json
Idempotency-Key: <stable activity request id>

{
  "requestId": "<same stable id>",
  "features": {"video": "upload:table:key"}
}
```

`Idempotency-Key` 存在时会成为 RPC 的 `request_id` 和 `feature.request_id`；若它与 body
中的 `requestId` 不一致，Gateway 返回 `400 IDEMPOTENCY_KEY_MISMATCH`。Temporal 重试
会复用由 workflow/run/activity 身份派生的稳定 key。

对 `kling-aigc-gpu-service` 当前实现的检查结果是：没有通用的 `request_id` 去重。
多数模型 executor 不接收该字段；IndexTTS 每次请求都会重新推理；ffmpeg 只用它生成
稳定的输出 blob key，计算仍会重复。因此 `maxAttempts` 省略或为 `0` 时，Worker 会按
`1` 处理。只有某个 route 明确证明可安全重试后，才应显式配置大于 `1`。

若以后需要在 Gateway 层保证重试幂等，必须使用跨 Pod 的持久化幂等记录；不能用单
Pod 内存缓存冒充生产去重。

成功响应：

```json
{
  "requestId": "agentk_...",
  "route": "transnet",
  "features": {"error_code": 0, "shots": "[[0,1.2]]"},
  "latencyMs": 127
}
```

主要错误状态：非法请求 `400`、请求过大 `413`、上游限流 `429`、KESS 不可用 `503`、
RPC 超时 `504`、MaaS 业务错误 `502`。Worker 会把确定的永久错误转为不可重试的
Temporal ApplicationError，其余网络、限流和不可用错误保留重试语义。

## 本地验证

```bash
cp configs/routes.example.json configs/routes.json
MAAS_GATEWAY_BEARER_TOKEN=dev-token go run ./cmd/maas-gateway
```

另一个终端启动 Worker（需要可连接的 Staging AgentK Temporal）：

```bash
cp configs/capabilities.example.json configs/capabilities.json
KWS_SERVICE_NAME=maas-worker-staging \
MAAS_GATEWAY_BASE_URL=http://127.0.0.1:8080 \
MAAS_GATEWAY_ALLOW_INSECURE_HTTP=on \
MAAS_GATEWAY_BEARER_TOKEN=dev-token \
go run ./cmd/maas-worker
```

构建与测试：

```bash
make test
make build
```
