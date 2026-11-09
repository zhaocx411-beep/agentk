# comic-drama（Agent Studio）

基于 agent-k 的下游 Agent 服务集：一个 API Server 承载 Agent 集市、
漫剧工坊（comic-drama）与六个固定 Profile 的通用 Chat Agent。
每个 Agent 是一个顶层目录（`meta.json` 展示元数据 + `profile*.json`
行为定义），共享的服务端与前端骨架在 `platform/` 下。

漫剧工坊**支持两种运行模式**，用 `DRAMA_MODE` 环境变量切换：

| 模式 | 控制流 | 外部依赖 | 适用场景 |
|---|---|---|---|
| `agent`（默认） | 一段长 system prompt 驱动的自由 Agent 会话，模型自主调用工具 | 仅 AgentK Connect API，开箱可用 | 快速接入 / 演示；支持对话式追加与修改 |
| `pipeline` | 确定性多阶段 Temporal Workflow，每阶段一个精调 prompt，顺序与校验由代码保证 | AgentK Connect API + **Worker 需与 AgentK 连同一 Temporal 集群** | 质量要求高的主通道 |

Pipeline 模式与
[storyboard-example](https://git.corp.kuaishou.com/kling-agent/agent-sys/agentk-contrib/storyboard-example)
同构但工序更完整，阶段划分与 prompt 移植自 agentq drama-workflow（qai-sdk）
验证过的工序，支持多集（最多 10 集）：

```text
分集剧本 → 人物定妆描述 + 全剧固定音色 → 切集（确定性正则，LLM 兜底）
→ 定妆三视图（并行）→ 逐集 child workflow（集间串行）→ 全剧成片

单集子流程（ComicDramaEpisode）：
资产描述（看前两集，判断复用）→ 台词精简 → 分镜（看前一集连续性）
→ 资产图：人物状态 / 场景 / 道具（并行）→ 逐镜首帧+视频（并行）→ 本集合成
```

## 仓库结构

```text
agents/                 全部 Agent 产品定义
  comic-drama/          漫剧工坊（唯一带独立代码的 Agent）
    meta.json           集市卡片与展示元数据
    profile-agent.json  agent 模式 Profile（自由发挥版，默认）
    profile-pipeline.json pipeline 模式 Profile（声明 comic_drama_film）
    *.go                漫剧 REST API（状态折叠、内存索引）
    web/                漫剧工坊前端（go:embed）
    worker/             Temporal Workflow（仅 pipeline 模式需要）
  creative-studio/ …    其余六个 Chat Agent：meta.json + profile.json
platform/
  core/                 配置、AgentK Connect 客户端、HTTP 工具、测试仿真
  chat/                 通用 Chat 服务与 Agent 注册表（聚合各目录 meta）
  market/               Agent 集市页（目录数据来自 /v1/agents）
  app/                  路由组装：漫剧 + Chat + 前端 + 健康检查 + 访问日志
cmd/
  apiserver/            API Server 入口（单进程承载全部 Agent）
  drama-worker/         漫剧 Temporal Worker 入口
catalog.go              编译期内嵌全部 Agent 的 meta 与 profile
docs/                   对外接入文档（前端全量消息 Chat 接口等）
```

新增一个 Agent：在 `agents/` 下建目录放 `meta.json` + `profile.json`，在
`catalog.go` 的 `agentOrder` 登记即可；chat 形态无需再写任何代码。

## 工作原理

### pipeline 模式（`DRAMA_MODE=pipeline`）

```text
POST /v1/dramas {brief, title?, stylePrompt?, maxShots?}
  → 创建 AgentK Session（固化 agents/comic-drama/profile-pipeline.json）
  → StartExecution(pipeline) 启动 comic_drama_film（模型引用由服务端按环境填写）
  → Worker 在 Temporal 上推进各阶段，逐阶段调用 agent-k 核心能力 Activity

GET /v1/dramas/{id}
  → GetExecution 拿 exec 级状态与最终资产清单
  → 遍历 ListEvents 分页，把 invocation.progress 事件折叠成阶段进度
```

| status | 含义 |
|---|---|
| `running` | 流水线仍在推进；`progress` 里是已完成阶段的汇报 |
| `completed` | 成片完成；`output` 是完整资产清单 |
| `failed` | 流水线失败（如全部镜头失败）；原因见 `error` |

单个镜头失败**不会**判死整次执行：成功镜头按原序合成，失败镜头的首帧与错误
原因保留在资产清单里。全部镜头失败才置为 `failed`。

### agent 模式（默认）

```text
POST /v1/dramas {brief, title?, stylePrompt?}
  → 创建 AgentK Session（固化 agents/comic-drama/profile-agent.json）
  → brief（+ 风格锚点）作为第一条用户消息驱动 Agent 自主创作

GET /v1/dramas/{id}
  → 增量拉取会话事件账本并折叠出快照（fold_agent.go）
  → status / messages（完整对话历史）/ assets（产物清单，URI 去重，
    带 stage 归列与模型写的 description）
  → edges（生成血缘：工具调用入参里的已知资产 URL → 本次产物）
  → pending（在途生成：正在跑的调用/任务及其输入资产）
  → stages（工序状态投影，见 stages.go）/ films / 产物计数

POST /v1/dramas/{id}/messages {text}
  → 继续对话：追加下一集、修改设定、要求重做（仅 agent 模式）
```

前端工坊页在 agent 模式下提供「创作画布 / Agent 输出」双视图。画布是
生成血缘 DAG（类 ComfyUI / tldraw image-pipeline 的观感，仅展示不交互）：
每张图、每个视频是一个节点，按工序分列（定妆·场景·道具 → 分镜首帧 →
镜头视频 → 成片）左右排布；谁参与了谁的生成（如两张定妆照 + 一张场景图
合成某镜首帧），节点间就连贝塞尔曲线。正在生成的目标显示为"生成中"
占位节点，输入到它的连线做红色流动动效。顶部保留五道工序的状态条。
输出视图展示完整对话往来，不再只留最后一段。

| status | 含义 |
|---|---|
| `running` | Agent 仍在推进：有未收敛的模型轮次或未闭合的工具调用（旧账本还包括未终态的后台任务） |
| `idle` | Agent 已收敛，等待下一条指令；本次产出见 `films` 与 `assistantSays` |
| `error` | 最近一次推进以 error / interrupted 收场；再发一条消息即可继续 |

响应 DTO 带 `mode` 字段，内嵌前端据此自动切换渲染（阶段时间线+资产清单 vs
Agent 汇报+继续对话）。

### 资产清单（output）

Pipeline 完成时返回全部中间产物，供下游存档或后续修改：

```json
{
  "title": "...", "synopsis": "...", "script": "完整分集剧本正文",
  "styleAnchor": "全剧统一视觉风格锚点",
  "characters": [{"name": "...", "prompt": "定妆生图 prompt", "imageUrl": "..."}],
  "voices": [{"name": "人物名或旁白", "timbre": "全剧固定嗓音描述"}],
  "episodes": [{
    "number": 1, "title": "第1集", "script": "本集剧本（台词精简后）",
    "assets": [{"category": "人物状态/场景/道具", "name": "...", "imageUrl": "..."}],
    "shots": [{
      "index": 0, "shotId": "01", "characters": ["..."], "scenes": ["..."],
      "speakers": ["..."], "firstFrame": "...", "storyboard": "...",
      "framePrompt": "...", "videoPrompt": "...", "durationSeconds": 6,
      "frameUrl": "...", "videoUrl": "...", "error": ""
    }],
    "film": "本集成片 URL", "clipCount": 2
  }],
  "film": "全剧成片 URL", "clipCount": 2
}
```

### 与 agentq 工序的对应

prompt 与关键约束逐条移植自 agentq drama-workflow（qai-sdk）：

- **定妆三视图 + 固定音色**（costume.ts）：每个人物一张正/侧/背三视图设定图
  （横版 16:9）锁定身份；同一次提取为每个有台词的人物（含旁白）定下全剧
  固定嗓音，逐镜按说话人注入视频 prompt，防止配音漂移。
- **切集**（split-script.ts）：优先按「第X集」标题行确定性切分，无标记且
  期望多集时才用 LLM 兜底。
- **分集资产**（asset-desc.ts / asset-images.ts）：每集生成人物状态 / 场景 /
  道具资产图；人物状态图走 `edit_image` 带一致性前缀 + 定妆三视图参考；
  资产描述阶段携带前两集剧本与资产上下文判断复用，复用直接引用前集 URL。
- **台词精简**（simplify.ts）：分镜前压缩超长对白。
- **分镜硬性规则**（storyboard.ts）：每集 ≤10 镜、每镜 5~10 秒、画面人物
  ≤3 人、台词字数按时长配额（5~6s ≤18 字，7~8s ≤26 字，9~10s ≤34 字）、
  first_frame 只描述起始静止画面；带上一集剧本与分镜连续性块。
- **首帧参考图逐图对应**（first-frames.ts）：≤4 张，按 人物状态图（无则
  定妆三视图）→ 场景 → 道具 的优先级注入，prompt 列出每张参考图的语义。
- **视频 prompt 结构**（video-clips.ts）：首帧锁定前缀 + 参考图逐图对应段
  （`@image_n` 指代，与首帧同一组参考图）+ 本片段说话人音色段 + 全剧风格段 +
  分镜正文原文。视频直调可灵 Omni 方言执行 activity
  （`kling_omni_video_execute`），首帧 + referImages 一起提交，与 qai 的
  首帧 + 参考图双保险一致。
- **风格锚点**（style.ts）：用户选定（或默认日漫赛璐珞风）的视觉风格注入
  每个生图、生视频 prompt，压住跨镜头画风漂移。

## 前置条件

**agent 模式**只需要：agent-k 的访问地址与 Service Principal API Key。

**pipeline 模式**额外需要向 agent-k 维护者获取：

- **agent-k 所连 Temporal 集群的地址与 Namespace**（Worker 必须与 agent-k
  共享同一 Temporal，才能收到 pipeline 调度并回调核心能力 Activity）；
- 或本地起 agent-k（`agentk-demo` + 本地 Temporal dev server，见 agent-k 仓库
  README），此时 Worker 直接连 `localhost:7233`。

API Server 通过环境变量指定 pipeline 使用的模型目录条目（capability ref），
缺省值对齐 agent-k demo 目录；目标实例目录不同请按实际覆盖：

| 环境变量 | 缺省值 | 用途 |
|---|---|---|
| `CHAT_MODEL` | `deepseek-v4-pro` | 剧本 / 人物 / 分镜文本生成 |
| `IMAGE_MODEL` | `z-image-turbo` | 定妆三视图（文生图） |
| `EDIT_MODEL` | `gemini-3-pro-image-edit` | 逐镜首帧（参考图生图） |
| `VIDEO_MODEL` | `kling-3.0-omni` | 逐镜视频。直调可灵 Omni 方言面（首帧 + 参考图），必须是可灵 Omni 条目 |

## 启动 Worker

```bash
export AGENTK_BASE_URL='https://agentk-api.staging.kuaishou.com'  # call_agent 直连 AgentK Connect
export AGENTK_API_KEY='<Service Principal API Key>'

go run ./cmd/drama-worker
```

Temporal 连接不需要配置：Worker 经 KESS 服务发现自动定位 agent-k 所连的
Temporal Frontend（服务 `klingai-muse-temporal-server`，namespace
`agentk`）。KESS 依赖运行环境的 kess-agent，因此 Worker 需在公司容器 /
物理机环境运行，无 kess-agent 的开发机上无法启动。

Worker 监听任务队列 `contrib.comic-drama-go`，注册两个能力：

- workflow `ComicDramaFilm`，发布为 capability `contrib.studio.comic_drama_film`
  （`agents/comic-drama/profile-pipeline.json` 的 pipelines 声明指向它）；
- workflow `StudioCallAgent`，即 capability `contrib.studio.call_agent`——统一对话
  入口（assistant）用它把任务路由到各子 agent（含漫剧）。**这条对话入口依赖
  此 Worker**：Worker 未运行时 call_agent 无法解析，assistant 会话无法创建。

> 私有 SDK 依赖（`agentk-sdk`、`infra-framework-go`）经公司 Go 代理拉取：
>
> ```bash
> export GOPROXY=https://goproxy.corp.kuaishou.com,direct GOSUMDB=off
> ```
>
> 开发机也可以走 git 直连：`go env -w GOPRIVATE='git.corp.kuaishou.com/*'`
> 并配置 git 的 ssh insteadOf。

## 启动 API Server

```bash
export DRAMA_MODE='agent'           # 默认；'pipeline' 需先部署 Worker
export AGENTK_BASE_URL='https://agentk-api.staging.kuaishou.com'
export AGENTK_API_KEY='<Service Principal API Key>'
export API_HOST='127.0.0.1'
export API_PORT='8080'

go run ./cmd/apiserver
```

漫剧 Profile 按模式使用编译期内嵌的缺省版本（pipeline →
`agents/comic-drama/profile-pipeline.json`，agent → `agents/comic-drama/profile-agent.json`），
需要热修 prompt 时用 `PROFILE_FILE` 指向磁盘文件覆盖。

连本地 `agentk-demo`（无鉴权 dev 租户）时不需要 API Key：

```bash
export AGENTK_BASE_URL='http://localhost:8080'
export API_PORT='8081'   # 避开 agentk-demo 的 8080
go run .
```

## Agent 集市与 Chat Agent

API Server 的 `/` 是 Agent 集市。除 `/agents/comic-drama/` 的结构化漫剧工坊外，
还提供七个固定 Profile、独立会话的 Chat Agent：

- `/agents/creative-studio/chat/`：完整创作助手；
- `/agents/media-creator/chat/`：图文视频创作；
- `/agents/web-research/chat/`：电商信息爬取；
- `/agents/document-research/chat/`：文档解析与研究；
- `/agents/video-understanding/chat/`：视频分镜理解；
- `/agents/audio/chat/`：语音合成与识别；
- `/agents/video-editor/chat/`：视频剪辑与生成。

这些 Agent 共用 Chat 交互与 API，但 Profile 均由服务端内嵌，客户端不能选择模型、
工具或提交 Profile override。浏览器只访问本服务，AgentK API Key 不会下发。

Chat API：

```text
GET  /v1/agents
POST /v1/agents/{agentId}/chats
GET  /v1/chats
GET  /v1/chats/{chatId}
POST /v1/chats/{chatId}/messages
POST /v1/chats/{chatId}/answers
GET  /v1/chats/{chatId}/events
GET  /v1/chats/{chatId}/stream
POST /v1/uploads
POST /v1/agents/{agentId}/converse
```

`converse` 是面向外部前端的无状态全量消息接口：调用方持有完整对话记录，每次
连同最新输入一起发来；服务端能续上 AgentK Session 就只投递增量，对不上（重启、
映射丢失、历史被改）就自动重建回灌。设置请求体 `stream: true` 或请求头
`Accept: text/event-stream` 可直接消费 AgentK 的实时文本 delta；原 JSON 长轮询
模式仍然兼容。接入方式见
[docs/converse-api.md](docs/converse-api.md)。

当 Agent 调用 `ask_user` 时，Chat 快照的 `pendingAsks` 会包含标准化后的问题和选项。
页面支持单选、多选及自定义答案，并通过 `/answers` 代理到 AgentK 的
`StartExecution(answers)`，提交后 AgentK 会恢复对应工作流。

Chat 页面支持点击或拖拽上传图片、视频、音频、PDF、Word、Excel、CSV、Markdown
和纯文本文件（单文件最大 256 MB）。业务服务使用
自己的 BlobStore/KCDN 配置，服务端匿名 PUT 对象，浏览器不接触存储凭证：

```bash
export COMIC_DRAMA_BLOB_ENDPOINT='https://bs3-hb1.staging.kuaishou.com'
export COMIC_DRAMA_BLOB_BUCKET='<业务共享桶名>'
# 可选；配置后上传响应直接返回公网 KCDN URL
export COMIC_DRAMA_BLOB_CDN_BASE='<KCDN 公网前缀>'
```

`read_file` 要读取未走公网 CDN 的私有 Blob URL 时，AgentK 必须使用完全相同的
`AGENTK_BLOB_ENDPOINT` 和 `AGENTK_BLOB_BUCKET`。否则该 URL 不会被识别为受管
artifact，而会进入公网 URL 安全检查并拒绝解析到 `10.x` 的内网地址。推荐两边同时
配置同一个 CDN 前缀：

```bash
export AGENTK_BLOB_ENDPOINT="$COMIC_DRAMA_BLOB_ENDPOINT"
export AGENTK_BLOB_BUCKET="$COMIC_DRAMA_BLOB_BUCKET"
export AGENTK_BLOB_CDN_BASE="$COMIC_DRAMA_BLOB_CDN_BASE"
```

未同时设置 `COMIC_DRAMA_BLOB_ENDPOINT` 和 `COMIC_DRAMA_BLOB_BUCKET` 时服务仍可启动，但上传
接口返回 `503`。配置不完整则启动失败，避免误把素材写到本机临时目录。

Chat 会话索引与漫剧任务一样暂存在当前进程内存中，服务重启后丢失；AgentK 侧
Session 和账本仍然存在。第一版没有恢复入口。

## 资产库

`/assets/` 聚合全部 Agent 服务的产物：按项目（一个漫剧任务或一个聊天会话）
归档，项目内按图片 / 视频 / 音频分列。仅展示与下载，无交互逻辑。

```text
GET /v1/assets
  → {projects: [{id, kind: drama|chat, title, agent, updatedAt, assets: [...]}]}
  → 读各服务的内存折叠现场，不回源 agent-k（资产随详情页轮询自然更新）

GET /v1/assets/download?uri=...
  → 服务端代理下载并带 Content-Disposition: attachment
  → 只放行已登记在任一项目清单里的 URI，不做开放代理
```

## Web 前端（漫剧工坊）

服务自带一个内嵌前端，启动后浏览器直接打开 `http://<host>:<port>/` 即可：

- 新建漫剧：填写标题（可选）与故事梗概/剧本，可从九个预设视觉风格中选一个
  （赛博朋克霓虹、定格动画黏土、日漫赛璐珞、古风3D国漫、皮克斯式3D、
  新中式水墨、写实电影、欧美漫画、治愈绘本）。选中的风格作为 `stylePrompt`
  提交，由 pipeline 注入每个生成阶段；不选则使用默认二维漫画风格。
- 任务详情：六阶段进度时间线（剧本 → 人物设定 → 定妆照 → 分镜 → 逐镜生成 →
  合成成片）、人物定妆图、逐镜片段在线播放、全剧成片、可展开的完整剧本。
  创作中的任务每 4 秒自动刷新。

页面通过 `go:embed` 打进二进制（`agents/comic-drama/web/index.html`），
部署时与 API 同进程同端口，无需额外静态文件目录，也没有跨域问题。

## API 用法

创建漫剧任务：

```bash
curl -sS -X POST http://127.0.0.1:8080/v1/dramas \
  -H 'Content-Type: application/json' \
  -d '{
    "brief": "深夜食堂的猫店长学会了开门",
    "stylePrompt": "治愈系手绘绘本风格，柔软笔触与纸张纹理",
    "maxShots": 4
  }'
```

立即返回 `202 Accepted`：

```json
{
  "id": "drm_...",
  "status": "running",
  "title": "深夜食堂的猫店长学会了开门",
  "progress": [],
  "createdAt": "...",
  "updatedAt": "..."
}
```

轮询任务（状态在 GET 时从 agent-k 刷新；终态后直接读本地缓存）：

```bash
export DRAMA_ID='drm_...'
curl -sS "http://127.0.0.1:8080/v1/dramas/$DRAMA_ID"
curl -sS http://127.0.0.1:8080/v1/dramas
curl -sS http://127.0.0.1:8080/healthz
```

运行中的响应带阶段进度：

```json
{
  "status": "running",
  "progress": [
    {"stepId": "script", "message": "剧本完成：猫店长", "at": "..."},
    {"stepId": "characters", "message": "人物设定完成，共 2 人", "at": "..."},
    {"stepId": "portrait/0", "message": "定妆完成：猫店长", "at": "..."}
  ]
}
```

完成后的响应带完整资产清单（见上文 output 结构）。

需要原始账本（进度明细、用量）时可从 Studio 一次读全量；服务端会遍历
AgentK `ListEvents` 的全部分页，并恢复为按 `eventId` 时间正序返回：

```bash
curl -sS "http://127.0.0.1:8080/v1/dramas/$DRAMA_ID/events"
```

agent 模式下还可以继续对话（追加下一集、修改设定、要求重做）：

```bash
curl -sS -X POST "http://127.0.0.1:8080/v1/dramas/$DRAMA_ID/messages" \
  -H 'Content-Type: application/json' \
  -d '{"text":"继续制作第 2 集"}'
```

> **样例限制：** 任务索引仅保存在当前进程内存中，服务重启后丢失（AgentK 侧
> 会话、pipeline run 与账本仍在，但本服务不再认识它们）；第一版 Endpoint 没有
> 入站鉴权，只能部署在可信内网。AgentK API Key 仅保存在服务端环境变量中，
> 不得下发给 API 调用方。

## 重要提示

运行本示例会实际调用文本、生图、图片编辑和视频生成能力。默认最多 8 镜，
每镜一次参考图生图（首帧）+ 一次视频生成，另有每人物一张定妆三视图。首次验证
建议 `maxShots` 传 2~3。

## 测试

```bash
go test -race ./...
```

apiserver 测试使用内存 fake AgentK（`platform/core/coretest`）；worker 测试用
Temporal 测试框架 mock 全部核心能力 Activity。都不会产生真实模型调用。

## 容器构建

从仓库根目录构建：

```bash
podman build --platform linux/amd64 \
  -f cmd/apiserver/Containerfile -t agent-studio-apiserver:dev .

podman build --platform linux/amd64 \
  -f cmd/drama-worker/Containerfile -t comic-drama-worker:dev .
```

运行：

```bash
export AGENTK_API_KEY='<Service Principal API Key>'
podman run --rm -p 8080:8080 \
  -e AGENTK_API_KEY \
  -e AGENTK_BASE_URL='https://agentk-api.staging.kuaishou.com' \
  agent-studio-apiserver:dev

# Temporal 经 KESS 自动发现，无需注入地址。
podman run --rm \
  -e AGENTK_API_KEY \
  -e AGENTK_BASE_URL='https://agentk-api.staging.kuaishou.com' \
  comic-drama-worker:dev
```

## 容器云部署

公司容器云（kcsize supervisor 模式）用 `deploy/container/Dockerfile`：

```bash
podman build --platform linux/amd64 \
  -f deploy/container/Dockerfile -t comic-drama-agent:dev .
```

镜像内 supervisor 拉起两个进程：`comic-drama-apiserver`（HTTP 服务，
kcsize 按 `/healthz` 做健康检查并注册 `AUTO_PORT0`）和
`comic-drama-worker`（Temporal Worker，注册 call_agent 路由与漫剧
pipeline，统一对话入口依赖它）。部署一个服务即可，环境变量两个：
`AGENTK_BASE_URL` 与 `AGENTK_API_KEY`（Service Principal Key）。worker
的 Temporal 连接经 KESS 自动发现，不需要配置。

worker 需要独立伸缩时，用 `deploy/container/worker.Dockerfile` 构建
单独的 worker 镜像另建服务（无端口无健康检查），并从合并镜像里删掉
`SUPERVISOR_PROGRAM1`。
