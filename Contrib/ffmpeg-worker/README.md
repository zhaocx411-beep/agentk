# ffmpeg-worker

把 kling-aigc-gpu-service 仓库的 ffmpeg kess 服务（`ffmpeg/`，kess 名
`maas-agentalgo-klingai-ffmpeg-tool`）注册成 agent-k 的一个 Capability。
接入方式照搬 storyboard-example 的 Python Worker：独立 Temporal Worker
监听自己的 Task Queue，通过 agent-k Python SDK 发布 Capability（Tool +
Pipeline），Workflow 内用 Activity 直调 kess 服务。

| 项 | 值 |
|---|---|
| Capability ID | `contrib.ffmpeg_tool` |
| Workflow Type | `FfmpegTool` |
| Task Queue | `contrib.ffmpeg-tool` |
| Tool / Pipeline 名 | `ffmpeg_tool`（Tool 为 blocking 模式，ffmpeg 操作通常秒级） |
| 后端 kess 服务 | `maas-agentalgo-klingai-ffmpeg-tool`（`infer.LocalInferService/Infer`） |

## 契约

输入（`abi.py:FfmpegToolInput`，op 白名单 29 个与
[kling-aigc-gpu-service 的 `ffmpeg/README.md`](https://git.corp.kuaishou.com/kling-agent/kling-aigc-gpu-service/-/blob/master/ffmpeg/README.md) 一致）：

```json
{
  "op": "cut",
  "params": {"start": 1.5, "end": 4.0},
  "video": "https://.../input.mp4"
}
```

或多步 pipeline（中间产物不经 blobstore）：

```json
{
  "pipeline": [
    {"op": "freeze", "params": {"duration": 3, "w": 720, "h": 1280}},
    {"op": "overlay_text", "params": {"text": "标题", "bbox_norm": [0.1, 0.8, 0.9, 0.95]}}
  ],
  "image": "db:upload:ylab-stunt:cover_key"
}
```

输入槽位 `video` `video2` `audio` `image` `subtitle` `font`（单文件）与
`videos` `audios`（列表）接受三种形态：

- **https URL**：bs3 blobstore 地址直接还原为 blob key；其他 URL 下载后
  base64 内联（上限 80MB，可用 `FFMPEG_INLINE_LIMIT` 调整）；
- **blob key**：`db:table:key`，原样透传（大文件推荐）；
- **base64**：`base64:xxx`。

输出：媒体产物为 1 个 Artifact（`uri` 为 IDC 内可下载地址，`params`
里带 `blobKey` / `ssoUri` / `outputSize`）；`probe` / `scene_detect` 等
分析类 op 的 JSON 在 `result`。

## 本地运行

前置条件与 storyboard-example 相同：向 agent-k 维护者获取开发集群
Temporal 地址与 Namespace；机器需可访问公司 PyPI（agentk-worker 已
vendor 成 wheel，无需访问公司 Git），且与 ffmpeg kess 服务在同一
kess division。

```bash
cd ffmpeg-worker
uv sync --all-extras

export TEMPORAL_ADDRESS='<开发集群 Temporal Server>'
export TEMPORAL_NAMESPACE='<开发集群 Namespace>'
uv run --no-sync python worker.py
```

启动成功打印：

```text
worker started: task queue "contrib.ffmpeg-tool", workflow "FfmpegTool"
```

环境变量（均有默认值）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `TEMPORAL_ADDRESS` / `TEMPORAL_NAMESPACE` | `localhost:7233` / `default` | 必配为开发集群的值 |
| `FFMPEG_KESS_NAME` | `maas-agentalgo-klingai-ffmpeg-tool` | 后端 kess 服务名 |
| `FFMPEG_BLOB_DB` / `FFMPEG_BLOB_TABLE` | `upload` / `ylab-stunt` | 产物上传库表 |
| `FFMPEG_RPC_TIMEOUT` | `600` | kess 同步调用超时（秒） |
| `FFMPEG_INLINE_LIMIT` | `83886080` | URL 下载内联上限（字节） |
| `BLOBSTORE_HTTP_BASE` | `http://bs3-hb1.internal` | 产物 artifact uri 前缀 |
| `BLOBSTORE_SSO_BASE` | `https://bs3.corp.kuaishou.com` | 产物 SSO 下载地址前缀 |

## 端到端联调脚本

Worker 连上开发集群后，用 API Key 一键验证（建 Session → 起 Pipeline →
轮询到终态）。API Key 放本目录 `.env`（已 gitignore）：

```bash
echo 'AGENTK_API_KEY=<Service Principal API Key>' > .env
uv run --no-sync python verify_agentk.py \
  --args '{"op":"probe","video":"db:upload:ylab-stunt:<key>"}'
```

## 通过 agent-k staging 验证

Worker 连上开发集群后，在 <https://agentk.staging.kuaishou.com> 创建
Session，粘贴 [`profile.json`](profile.json) 作为 Session Profile，
即可让 agent 以 Tool 方式调用 `ffmpeg_tool`，或直接启动
`ffmpeg_tool` Pipeline，Args 示例：

```json
{
  "op": "probe",
  "video": "db:upload:ylab-stunt:your_video_key"
}
```

## 测试

```bash
uv run --no-sync pytest
```

Workflow 测试用 Temporal time-skipping 环境 + 假 Activity；kess 桥接层
测试 mock gRPC stub，不依赖内网服务。

## 构建镜像

本仓库完全自包含，构建时只访问公司内部源（KCI 经典 docker builder
可直接构建，无需 BuildKit / SSH）：

- kess 协议 pb2 桩代码 vendor 在 `protos/`（源头是 kling-aigc-gpu-service
  仓库的 `kess_client/protos`，协议变更时同步一份即可）；
- agentk SDK vendor 在 `vendor/agentk_worker-*.whl`（源头是 agentk-sdk
  仓库的 `python/worker`，升级时重新 `uv build --wheel` 一份并更新
  `pyproject.toml` 里的 rev 注释）；
- 基础镜像与 Python 包均来自 registry.corp / pypi.corp。

构建上下文即 repo 根：

```bash
docker build --platform linux/amd64 -t ffmpeg-worker:dev .
```

## 注意

- Worker 本身无状态、不监听端口，可多副本水平扩容（同一 Task Queue）。
- 大媒体请传 blob key 或 bs3 URL，避免 80MB 内联上限与 gRPC 消息上限。
- 生产使用文字类 op（`overlay_text` / `title_card`）时，注意后端服务
  容器内的字体问题（见 kling-aigc-gpu-service 的 `ffmpeg/README.md`），
  必要时通过 `font` 槽位传字体。
