# Narrato AgentK 测试用例与逐阶段 I/O 检查表

本文用于补充自动测试，并作为公司内网 AgentK E2E 验收清单。时间统一使用秒，URI 必须为 AgentK Artifact/CDN URL 或 Core 允许的安全 HTTP(S) URL。

## 1. 一键 Pipeline 输入输出

输入示例：

```json
{
  "videoUri": "https://cdn.example/source.mp4",
  "theme": "自然纪录片",
  "instructions": "语气克制，不虚构",
  "aspect": "9:16",
  "voiceId": "Cherry",
  "narrationVolume": 0.8,
  "subtitles": true
}
```

预期输出：

```json
{
  "script": {
    "version": "1",
    "sourceVideoUri": "https://cdn.example/source.mp4",
    "sourceDurationSeconds": 60.0,
    "theme": "自然纪录片",
    "segments": []
  },
  "artifacts": [
    {
      "type": "video",
      "uri": "https://cdn.example/final.mp4",
      "description": "纪录片解说成片"
    }
  ],
  "durationSeconds": 42.5,
  "segmentCount": 6
}
```

检查点：

- `sourceVideoUri` 必须保持调用者输入，不接受模型改写。
- `durationSeconds` 是所有实际渲染片段时长之和，不一定等于源视频时长。
- `segmentCount` 必须等于脚本片段数。
- 一键 Workflow 的脚本输出必须原样传给渲染子 Workflow。

## 2. 脚本生成 Workflow

| 步骤 | Activity 输入 | Activity 输出 | 本地检查 |
|---|---|---|---|
| 输入校验 | Pipeline JSON | 规范化 `videoUri/theme/instructions` | HTTP(S) URI、无额外业务参数 |
| 视频探测 | `probe_video: {videoUrl}` | `fileSizeBytes/durationSeconds/videoStreams` | 1–512 MiB、时长大于 0、至少一个视频流 |
| 窗口切分 | `durationSeconds` | `[startSeconds,endSeconds]` | 默认 30 秒，尾段按实际时长截断 |
| 视频理解 | `model/videoUrl/startSeconds/endSeconds/prompt` | `text/model/params` | `text` 非空；窗口时间采用本地值，不采用模型声称的时间 |
| 脚本生成 | `model/prompt/outputSchema` | `result` | 最多一次结构化重试 |
| 脚本规范化 | 模型 JSON | 完整脚本 JSON | URI/时长/主题使用可信输入，ID 重排，自动 OST 强制为 2 |

候选用例：

| ID | 场景 | 输入/Mock | 预期 |
|---|---|---|---|
| G01 | 65 秒视频 | probe 时长 65 | 分析窗口为 `0–30`、`30–60`、`60–65` |
| G02 | 恰好 30 秒 | probe 时长 30 | 只调用一次视频理解 |
| G03 | 文件过大 | `fileSizeBytes=512MiB+1` | `InvalidArgument`，不调用视觉模型 |
| G04 | 无视频流 | `videoStreams=[]` | `InvalidArgument` |
| G05 | 视觉结果为空 | `text=""` | `InvalidModelOutput`，不可重试 |
| G06 | 模型返回重复 ID | 两段同 ID | 本地校验失败，重新调用文本生成一次 |
| G07 | 模型返回越界时间 | `sourceEndSeconds > duration` | 重试一次后仍错误则 `InvalidModelOutput` |
| G08 | 模型改写源 URI | result 中使用其他 URI | 最终脚本仍使用调用者 `videoUri` |
| G09 | 模型返回 OST 0/1 | 结构正确 | 自动脚本最终统一改为 OST 2 |
| G10 | 用户取消 | 分析过程中取消 | 执行终态为 `CANCELLED`，进行中的 Activity 收到取消 |

## 3. 渲染 Workflow

示例脚本：

```json
{
  "version": "1",
  "sourceVideoUri": "https://cdn.example/source.mp4",
  "sourceDurationSeconds": 20.0,
  "theme": "自然",
  "segments": [
    {
      "id": "segment-001",
      "sourceStartSeconds": 0.0,
      "sourceEndSeconds": 5.0,
      "narration": "静音原声并配音",
      "ost": 0
    },
    {
      "id": "segment-002",
      "sourceStartSeconds": 5.0,
      "sourceEndSeconds": 8.0,
      "narration": "只保留原声",
      "ost": 1
    },
    {
      "id": "segment-003",
      "sourceStartSeconds": 8.0,
      "sourceEndSeconds": 14.0,
      "narration": "原声和配音共存",
      "ost": 2
    }
  ]
}
```

假设两段 TTS 的真实时长分别为 2 秒和 4 秒，预期中间 I/O：

| 片段 | TTS | process_video operations | 实际时长 | 原声 |
|---|---|---|---:|---|
| OST 0 | 调用 | `trim(0,2) + mute` | 2 | 移除 |
| OST 1 | 不调用 | `trim(5,3)` | 3 | 保留 |
| OST 2 | 调用 | `trim(8,4)` | 4 | 保留 |

预期 compose 时间轴：

```json
{
  "audioTracks": [
    {"url": "s0.mp3", "startSeconds": 0.0, "durationSeconds": 2.0},
    {"url": "s2.mp3", "startSeconds": 5.0, "durationSeconds": 4.0}
  ],
  "subtitleSegments": [
    {"startSeconds": 0.0, "endSeconds": 2.0, "text": "静音原声并配音"},
    {"startSeconds": 5.0, "endSeconds": 9.0, "text": "原声和配音共存"}
  ],
  "preserveOriginalAudio": true
}
```

候选用例：

| ID | 场景 | 输入/Mock | 预期 |
|---|---|---|---|
| R01 | OST 0 | TTS 2 秒 | trim 使用 2 秒，追加 mute，生成字幕 |
| R02 | OST 1 | 脚本区间 4 秒 | 不调用 TTS，不 mute，不生成配音字幕 |
| R03 | OST 2 | TTS 3.5 秒 | trim 使用 3.5 秒，保留原声，生成字幕 |
| R04 | 非 source 画幅 | `aspect=1:1` | 每段追加 `reframe 1080×1080 cover` |
| R05 | 关闭字幕 | `subtitles=false` | `subtitleSegments=[]` |
| R06 | TTS 无 Artifact | `artifacts=[]` | `InvalidArgument`，不调用 process_video |
| R07 | TTS 无真实时长 | 无 `audioLengthMs` | `InvalidArgument`，不猜测时长 |
| R08 | TTS 超过剩余视频 | `start + TTS duration > sourceDuration` | 明确失败，不静默截断 |
| R09 | process 无 Artifact | Core 返回空产物 | `InvalidCoreOutput` |
| R10 | compose 无 Artifact | Core 返回空产物 | `InvalidCoreOutput` |
| R11 | 1–8 段 | 单批 compose | 直接返回 compose Artifact，不调用 concat |
| R12 | 9 段 | 两批 compose | 批次为 8+1，最终调用一次 concat |
| R13 | 17 段 | 三批 compose | 批次为 8+8+1，concat 输入顺序稳定 |
| R14 | 人工脚本源时长不一致 | probe 与脚本差值大于 0.5 秒 | `InvalidArgument` |
| R15 | narrationVolume 边界 | 0、2 | 接受并原样进入 `audioTracks.volume` |
| R16 | narrationVolume 越界 | -0.1、2.1、NaN | 输入校验失败，不调用 Core |

## 4. 上传 API

| ID | 请求 | 预期响应 |
|---|---|---|
| U01 | `GET /healthz` | `200 {"status":"ok"}` |
| U02 | multipart MP4 | `201`，返回 URI、SHA-256、字节数、MIME |
| U03 | 相同内容重复上传 | 第二次命中相同 key，不再次 PUT |
| U04 | 超过 512 MiB | `413`，临时文件被清理 |
| U05 | 空文件 | `422` |
| U06 | `text/plain` | `422` |
| U07 | MIME 为 MP4、扩展名为 `.exe` | `422` |
| U08 | 文件名含 `../../` | Blob key 仍只由 SHA-256 组成 |
| U09 | BS3 PUT 5xx | API 返回 5xx，不伪造 URI |
| U10 | 已配置访问令牌但请求未携带/错误 | `401`，不调用上传服务 |
| U11 | 非回环监听且没有访问令牌 | API 启动期失败，不开放端口 |

成功响应必须符合：

```json
{
  "uri": "https://cdn.example/narrato-agentk/uploads/<sha256>.mp4",
  "sha256": "<64 hex>",
  "sizeBytes": 123456,
  "contentType": "video/mp4"
}
```

## 5. CLI 与事件流

| ID | 操作 | 检查点 |
|---|---|---|
| C01 | `session create` | Profile 恰好包含三个 Pipeline，默认文本模型正确 |
| C02 | `run --file` | 先上传，再创建/复用 Session，再启动 `documentary_video` |
| C03 | `script generate` | Pipeline 输出完整写入 UTF-8 JSON 文件 |
| C04 | `render --script` | JSON 脚本完整放入 `documentary_render.args.script` |
| C05 | watch 断线 | `list_events` 分页恢复，按时间顺序打印且 event ID 去重 |
| C06 | `Ctrl-C` | 只停止本地观察，远端仍为 RUNNING |
| C07 | `cancel` | 调用 `CancelExecutionRequest(session_id)`，终态为 CANCELLED |
| C08 | download | 分块写入目标文件，HTTP 错误不留下伪成功信息 |
| C09 | CLI 上传且已配置令牌 | 自动附加 `X-Narrato-Console-Token` |
| C10 | Artifact URI 使用非 HTTP(S) 协议 | 输出 JSON 可审计，但前端不生成可点击链接 |

## 6. Worker、重放与内网 E2E

| ID | 场景 | 预期 |
|---|---|---|
| W01 | Worker manifest | 三个 Capability ID、Workflow Type、Schema、超时完全匹配 |
| W02 | Core Capability 缺失 | Worker 启动失败，不发布贡献 Capability |
| W03 | Core Schema 字段缺失 | Worker 启动失败并指出不兼容 Capability |
| W04 | Temporal 重放 | 三个 Workflow 均无不确定性错误 |
| W05 | Worker 重启 | 运行中的执行从 Temporal 历史恢复 |
| E01 | 短视频脚本生成 | 事件阶段完整，最终脚本可导出 |
| E02 | 人工修改脚本再渲染 | 最终 MP4 可下载，修改内容生效 |
| E03 | 一键成片 | 一条 CLI 命令得到最终 MP4 |
| E04 | 媒体检查 | ffprobe 显示 MP4/H.264、期望画幅和音轨 |
| E05 | 字幕检查 | 抽帧能看到烧录字幕，关闭字幕后抽帧无字幕 |
| E06 | 取消 | 中途取消后 AgentK 终态严格为 `CANCELLED` |

内网 E2E 建议分别准备 10 秒、65 秒、9 片段和接近 512 MiB 四类样片，以覆盖短尾窗口、多批 compose、大小边界与取消恢复。
