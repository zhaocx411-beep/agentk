# AgentK Capability Demo

一个基于 AgentK **Python SDK** 的本地测试页面，用于：

- 从运行中的 AgentK 动态读取公开 Core capability 和 JSON Schema，并自动生成输入表单。
- 执行 capability、查看事件、结构化输出与媒体产物，并支持取消。
- 测试 `comic-drama` 中的 9 个 agent-loop case：统一入口、漫剧制作、完整创作、图文视频、网页研究、文档研究、视频理解、音频、视频剪辑。
- 测试 Agent 100 项目；当前内置 Narrato 纪录片脚本生成、脚本渲染和一键成片。
- 展示多轮消息、reasoning、工具调用、invocation progress、`ask_user`、错误与产物。

本项目从快手内部 prod PyPI 安装 AgentK Python SDK 0.3.7.dev3
解析，不依赖本地 `agentk-sdk` 源码目录。SDK 不再使用 Bearer API Key。本地调试必须设置
`KWS_SERVICE_NAME=klingai-agentk-capability-demo`；云端环境直接使用 KSN 验证服务身份，
无需配置 Auth Key。旧的 `AGENTK_API_KEY` / `--api-key` 配置仍可被 demo 解析以兼容
现有启动脚本，但不会传给 SDK。

## 完整本地启动

下面是已经验证通过的本地拓扑：`agentk-standalone` 在一个进程内启动 API、Core Worker、
临时 Store 和内嵌 Temporal，监听 `8081`；本测试页面监听 `8080`。不要使用生产入口
`cmd/agentk-api` 代替 standalone：生产入口只接受 MySQL/KDB，且不包含 Worker。

### 1. 启动 AgentK

在 AgentK 仓库中执行：

```bash
cd ~/Coding/agent-k/standalone

env -u KWS_LANE_ID \
  AGENTK_MODELS=../models.example.yaml \
  AGENTK_BLOB_ENDPOINT='https://bs3-hb1.staging.kuaishou.com' \
  AGENTK_BLOB_BUCKET='klingcorp-agentk-engine' \
  AGENTK_KEFFECTS_WORK_BUCKET='klingcorp-agentk-engine' \
  AGENTK_KCDN_PROJECT='agentk-engine-cdn' \
  AGENTK_KCDN_UPLOAD_URL='https://kcdn.corp.kuaishou.com/api/kcdn/v1/service/npmUpload/single' \
  AGENTK_KCDN_UPLOAD_TOKEN="$AGENTK_KCDN_UPLOAD_TOKEN" \
  AGENTK_KCDN_DISPATCH_URL='https://cdn-dispatch-access.test.gifshow.com/api/v1/dispatch' \
  AGENTK_KCDN_EFFECTIVE_SECONDS='3600' \
  go run ./cmd/agentk-standalone --bind-address 127.0.0.1 --port 8081
```

`AGENTK_BLOB_REGION` 不配置时默认使用 `hb1`。上述 `models.example.yaml` 符合当前严格
schema，但需要设置其中引用的 API Key 环境变量；测试 Gemini 时还需复制并填写示例中的
Gemini 条目。若改用私有目录，其中不能包含旧版 `contract`、`transport`、`serviceName`、
`timeoutSeconds` 字段，否则启动会直接失败。

上面的 upload token 必须事先由 KSecret 或本地安全环境注入，不能把真实值写进命令、
README 或仓库。该配置只用于本地/staging 把 BlobStore 文件复制到生产 KCDN，再经 test
dispatch 签发公网防盗链地址；生产 IDC 应同时取消 `AGENTK_KCDN_UPLOAD_URL` 和
`AGENTK_KCDN_UPLOAD_TOKEN`，并把 dispatch 地址设为内部生产 endpoint。两套完整配置见
AgentK README 的“BlobStore 与 KCDN”。

确认 AgentK 在线：

```bash
curl -fsS http://127.0.0.1:8081/healthz
```

### 2. 启动 Narrato Worker（使用 Narrato 时必需）

AgentK standalone 和测试页面都不会代替 Narrato Worker。要使用 Agent 100 / Narrato，
必须另开终端启动 Worker，并保持进程运行。先从最新的 AgentK ready 日志中找到
`temporal=127.0.0.1:<port>`；standalone 每次重启都会重新分配该端口，因此也必须使用
新端口重启 Narrato Worker：

```bash
cd ~/Coding/agentk-capability-demo
export NARRATO_CONFIG="$PWD/config.narrato.yaml"

env -u KWS_LANE_ID \
  KWS_SERVICE_STAGE=STAGING \
  KWS_SERVICE_NAME=klingai-agentk-capability-demo \
  NARRATO_TEMPORAL_ADDRESS=127.0.0.1:<standalone-temporal-port> \
  NARRATO_TEMPORAL_NAMESPACE=agentk \
  NARRATO_WORKER_TASK_QUEUE=demo-worker \
  NARRATO_CORE_TASK_QUEUE=agentk \
  uv run agentk-capability-demo-worker narrato
```

首次使用时先执行 `cp config.narrato.example.yaml config.narrato.yaml` 并填写配置。
如果启用了个人泳道，还需要同时启动 baseline 和 lane 两个 Worker，详见下文
“Agent 100 / Narrato”。

### 3. 启动测试页面

要求 Python 3.11–3.13、`uv`，并能访问快手内部 prod PyPI。`uv sync` 会安装线上
发布的 AgentK SDK 0.3.7.dev3，不需要本地 `agentk-sdk` checkout：

```bash
uv sync --dev

env -u AGENTK_DEMO_BLOB_CDN_BASE \
  KWS_SERVICE_NAME=klingai-agentk-capability-demo \
  AGENTK_ADDRESS=http://127.0.0.1:8081 \
  AGENTK_DEMO_BLOB_ENDPOINT='https://bs3-hb1.staging.kuaishou.com' \
  AGENTK_DEMO_BLOB_BUCKET='klingcorp-agentk-engine' \
  uv run agentk-capability-demo --host 127.0.0.1 --port 8080
```

打开 <http://127.0.0.1:8080>。

页面的 `LIVE CATALOG` 来自 AgentK 的实时 `ListCapabilities`，因此 AgentK 或
Contrib capability 不在公开 Capability Lab 中展示；Narrato 等专项流程通过
Agent 100 独立入口测试，不与同步 Core capability 混在同一目录。

也可以使用环境变量：

```bash
KWS_SERVICE_NAME=klingai-agentk-capability-demo \
AGENTK_ADDRESS=http://127.0.0.1:8081 \
AGENTK_DEMO_BLOB_ENDPOINT='https://bs3-hb1.staging.kuaishou.com' \
AGENTK_DEMO_BLOB_BUCKET='klingcorp-agentk-engine' \
uv run agentk-capability-demo
```

在公司部署环境中不要设置 `AGENTK_ADDRESS`。Demo 会调用 Python SDK 的 gRPC
client factory，通过 KESS 服务名 `klingai-agentk-api` 自动发现并直连 AgentK API
实例，并由云端 KSN 自动验证服务身份，不需要设置 Auth Key。`AGENTK_ADDRESS` 只用于
本地联调或显式指定测试网关。

如果使用已经运行的远端 AgentK，只需要把 `AGENTK_ADDRESS` 或
`--agentk-address` 改成对应地址，不需要在本机启动 Temporal 和 AgentK。

## 文件上传

上传内容先保存到本地 `uploads/`。视频文件上限为 100MB，图片、音频和文档上限为
20MB；这是 Demo 浏览器和上传服务的边界，不是 Gemini 或其他 provider 的 URL 输入
硬限制。为让 `edit_image` 等能力把输入识别为受管 artifact，推荐让 demo 和 AgentK
使用相同 BlobStore；demo 的对象固定写入 `agentk-capability-demo/` 二级目录：

```bash
export AGENTK_DEMO_BLOB_ENDPOINT='https://bs3-hb1.staging.kuaishou.com'
export AGENTK_DEMO_BLOB_BUCKET='klingcorp-agentk-engine'
unset AGENTK_DEMO_BLOB_CDN_BASE
```

按上述配置，Demo 返回稳定的 `<endpoint>/<bucket>/<key>` BlobStore URL，不返回临时 CDN URL。
AgentK 只要求 URL host 与自身 `AGENTK_BLOB_ENDPOINT` 一致，bucket 可以不同；把文件
交给外部模型时，再由 AgentK 使用 `agentk-engine-cdn` 实时签发临时公网地址。
当前版本不再读取 `AGENTK_DEMO_BLOB_CDN_BASE` 或旧的 `AGENTK_BLOB_CDN_BASE`；启动前
清理它们可以避免误用旧版进程、绕过 BlobStore 识别与实时签发链路。BlobStore 上传使用
分块流式 PUT，不会把整个大文件一次性读入内存。

本次接入的 Gemini 媒体理解、Gemini/Qwen 图片编辑、Seedance，以及 Gemini Chat 视频和音频，
会把受管 BS 素材实时签发为 KCDN URL；文件大小已知、未知或较大都不会使正常请求切换成
base64。Gemini Chat 的视频和音频使用远程 `fileData`，URL 签发或 Range/MIME 探测失败会
直接报错，不做 inline 重试；图片仍以内联数据发送。`understand_media` 对图片、视频、音频
采用 URL 优先策略，只有远程抓取失败且所有回退内容合计不超过 15 MiB 时才会 inline 重试。
HTTP 413、鉴权、限流、参数和内容安全错误绝不 inline 回退。Seedance 的媒体输入是
URL-only；Azure ASR 与 GPT-image edit 使用
provider 要求的 multipart 二进制；生产 Kling workflow 直接使用 BlobStore bucket + key。
后面三类都不应被误解为 base64 fallback。

如果不配置 BlobStore，当 AgentK 不在同一台可访问主机上时，需要传入一个它能够
访问的公开地址：

```bash
KWS_SERVICE_NAME=klingai-agentk-capability-demo \
uv run agentk-capability-demo \
  --agentk-address https://agentk.example.test \
  --public-base-url https://demo.example.test
```

页面始终把上传文件 URL 写入消息正文。只有所选主模型声明支持相应类型时，图片、视频和
音频才额外通过通用的 SDK `MediaAttachment` 提交；视频每轮最多附加第一个，其他视频继续
以 URL 供工具使用。AgentK 对一轮主模型输入中的图片、视频、音频合计最多接受 10 个，
超过限制时页面展示 AgentK 返回的校验错误。URL/schema 字段中的上传结果直接写回该字段。

## 页面说明

### Capability Lab

1. 左侧 catalog 来自 SDK `ListCapabilities`，自动处理分页。
2. 表单根据 `inputSchema` 生成，支持字符串、数字、布尔、枚举、数组、对象和 URL。
3. 表单优先使用 schema 自带的 `default` / `examples`，并为其余必填参数生成可编辑示例；
   `example.com` URL 和 Maas 部署别名需要按本地环境替换。Qwen、MiniMax 的系统音色由
   AgentK 内置候选生成下拉框；Contrib、`generate_brand_ad` 与 CosyVoice
   不在公开 capability catalog 中展示。
   选填参数不会被示例自动开启。
4. 图片、视频、音频、字幕 URL 字段可直接上传；URL 数组支持多选并追加上传结果，仍可手工编辑 JSON。
5. 表单和 Raw JSON 在提交前统一校验 schema 的必填项、类型、枚举、数值/长度范围、数组数量、URI 和组合约束；提交失败时会显示全部问题并标记对应字段。
6. 可随时点击“恢复示例”，复杂 schema 也可切换到 Raw JSON。
7. 图片、视频和音频输出会直接显示预览或播放器，其他文件提供打开链接；完整结果仍可展开 Raw JSON。
8. 每次运行创建独立 session，通过 pipeline execution 调用选中的 capability。

#### Keffects 关联能力测试顺序

Capability Lab 已覆盖以下两条关联链路，字段仍以 AgentK 实时返回的 schema 为准：

1. 可选先调用 `agentk.create_voice_embedding`，上传清晰人声音频并取得
   `voiceEmbeddings`。该 capability 仅部署在 PRT，本地或 staging catalog 不出现时属于预期；
   它的输出是音色特征，不是口型驱动音频。
2. 调用 `agentk.create_kling_subject`，在 `images`（1~3 张）和 `video` 中二选一，按需传入
   上一步的 `voiceEmbeddings`。把输出的 `subjectLists` 传给已有的
   `agentk.kling_omni_video.subjectLists`，即可测试主体一致性视频生成。
3. 另一条链路先调用 `agentk.identify_faces`，从输出中记录 `faceIndexKey`，并选择
   `firstFaceId` 或 `faces[].id`。
4. 调用 `agentk.lip_sync_video`，传入原视频、真实驱动音频、上述人脸索引以及毫秒单位的
   `soundStartTime` / `soundEndTime`。页面会在提交前检查结束时间必须大于开始时间。

`audio`、`video`、`images` 等真实素材字段支持直接上传；`voiceEmbeddings`、`subjectLists`、
`faceIndexKey` 和 `faceId` 是前序 capability 的结构化输出，应从前一次运行结果复制，不会被
误显示为文件上传项。

### Agent Loop

1. 选择一个 `comic-drama` case。
2. 首条消息会用相应 profile 创建 session；后续消息复用该 session。
3. 页面轮询 ledger，展示 assistant text/reasoning、工具状态、进度、产物和问题。
4. `ask_user` 会转换成逐题必答的选项/自定义答案表单；等待选择时锁住普通消息，
   提交后通过 answer execution 继续同一会话，并在 `ask_user.responded` 后关闭卡片。
5. 支持点击或拖拽上传图片、音视频和文档；显示上传状态与图片缩略图。素材都会按
   `文件名（类型）：可访问 URL` 写入正文；支持原生媒体输入的主模型还会收到
   `MediaAttachment`，其余 Agent 仍通过 URL 调用理解、处理工具。
6. Agent 执行期间会锁住消息、文件选择和发送按钮，避免同一 Session 重复启动 execution。

### 指定主模型

Agent Loop 页面当前没有模型下拉框，但创建 Session 的 API 已支持显式指定主模型；
`model` 会覆盖所选测试 Agent 的默认模型：

```bash
curl -fsS http://127.0.0.1:8080/api/agent-sessions \
  -H 'content-type: application/json' \
  -d '{"agentId":"gemini-creative","model":"gemini-3-flash-preview"}'
```

执行事件中的 `roundStarted.model` 应与请求值一致。Capability Lab 中由 capability schema
暴露的 `model` 字段仍在运行参数中直接选择，不走这个 Session 参数。

当前主模型原生视频和音频输入只支持 `gemini-3.1-pro-preview`、
`gemini-3-flash-preview`、`gemini-3.1-flash-lite`；配置来源是 AgentK
`internal/modelcontract/contract.go`，并且模型还必须出现在 `AGENTK_MODELS` 指向的目录中。
AgentK 主模型入口当前每轮最多接收一个视频，图片、视频、音频合计最多 10 个；图片仍受
Profile 的 `maxImagesPerRound` 限制。音频支持 WAV、MP3/MPEG、AIFF、AAC、OGG、FLAC、
M4A、L16、Opus、ALAW、MULAW 和 WebM，并规范化常见 `audio/x-*` 别名。AgentK 不下载
音频计算时长，Gemini 的 9.5 小时总时长限制由 provider 校验。官方对外部 URL 或 signed
URL 视频建议单文件不超过 100 MB；这是 provider 使用建议，不是 AgentK 对 URL/fileData
的硬编码前置大小限制。

### BlobStore/KCDN E2E 记录

先明确测试口径：当前 AgentK Core manifest 有 23 个 capability，本页公开 21 个。
2026-08-30 用当前本地 AgentK、当前本项目和本地 editable SDK 逐项调用全部 21 个：
首次运行 18 个成功，`generate_video`、`kling_image_to_video`、`kling_omni_video` 因
AgentK 业务桶被误用作 Keffects 工作桶而触发远端 Java `NullPointerException`；同轮
`kling_text_to_video` 成功。将 `AGENTK_KEFFECTS_WORK_BUCKET` 改回 Keffects 专用的
`upload-ylab-stunt` 后，相同输入的 I2V 和三参考图 Omni 均真实重跑成功，产物回存
`klingcorp-agentk-engine`。`generate_video` 的图生视频分支复用同一 I2V 实现。
`generate_brand_ad` 和 `generate_speech_cosyvoice` 已按产品决定隐藏，不计入这 21 项。
Seedance 受管图片/视频/音频输入与输出回存、Gemini edit CDN 路径、文档 Agent Loop
和 Gemini 创作助手均已完成真实 E2E。AgentK 全量 test/vet、核心包 race test、本项目
176 项 pytest 和 50 项前端 Node 测试均通过。

2026-08-28 至 2026-08-29 使用 `klingcorp-agentk-engine` bucket 和
`agentk-engine-cdn` 项目同时启动
AgentK 与本项目后，验证结果如下：

- 上传接口成功把 49.5 MB MP4 写入新 bucket，并返回稳定 BlobStore URL；AgentK 能将其
  识别为受管文件并进入 KCDN 签发流程。
- 显式指定 `gemini-3-flash-preview` 成功，所有 `roundStarted` 事件均记录了该模型。
- Gemini 助手完成 `generate_image` 后依次调用 `list_artifacts`、`view_images`，能够描述
  提示词没有给出的耳机、杯子、便签文字等画面细节，证明读取的是实际产物内容。
- 最初“受管视频由 Gemini 直接理解”被 KCDN 环境不匹配阻塞：生产 `.internal`
  dispatch 只能从 IDC 访问；生产 REST upload 的裸地址和手工替换域名均返回 403；
  staging dispatch 对该生产副本回源 404。
- 办公网的正确路径是 AgentK 从受管 BS 流式读取并上传到生产 KCDN，再把 upload 响应的
  path 交给 test dispatch。实测签发地址公网 Range GET 返回 206 和正确 MIME；AgentK
  只把签发 URL 交给外部模型，Event/Artifact 仍保存稳定 BS URL。
- 防盗链地址对 HEAD 可能返回 404，但 `Range: bytes=0-0` GET 正常返回 206；AgentK 直接
  做一字节 Range 探测并从 `Content-Range` 读取真实大小，不会下载整段视频。
- 0.8 MB MP4 经本页上传到 `klingcorp-agentk-engine` 后，显式选择
  `gemini-3.1-flash-lite` 的普通助手在零工具调用下准确识别猫头机器人、图书馆和放书动作；
  `understand_media` 同一素材回报 `delivery=url`，证明没有走 base64/inline 回退。
- 旧版 92 MB E2E 的 HTTP 413 根因是旧传输逻辑把受管 BS 视频编码为 base64，膨胀后的
  请求体被上游拒绝。修复后，完整 92 MB 文件确实先以 KCDN `fileData` 发送；当前 Kigress
  Gemini 部署随后返回 `HTTP 400 / File content exceeded the size limit / max_bytes_fetched:
  15728640`。这是上游网关 15 MiB 拉取限制，不是 AgentK 的 413，也不能改走 base64。
- Narrato 本来就按时间窗理解视频。`probe_video.fileSizeBytes` 超过上述部署上限时，它会先用
  `process_video` 裁出当前窗口并写回稳定 BS，再由 AgentK 签发新的 CDN URL；这不是 inline
  fallback，也不会改变其他 capability 的通用传输策略。真实 30 秒裁片为 2,429,149 字节，
  `gemini-3.1-flash-lite` 返回 `delivery=url`，准确识别了《钢铁侠》片头、军用悍马、阿富汗
  库纳尔省和中英字幕。
- 此前因裸 CDN 403 失败的 Qwen `edit_image` 在两段签发链路下成功读取参考图，结果仍回存
  为新 bucket 下的稳定 BlobStore URL。
  E2E 必须同时检查公网可达、MIME/Content-Range 和模型对真实画面的描述；仅看到
  upload API 成功或模型走 inline 回退都不算 URL 链路通过。

最终 92 MB E2E 于 2026-08-29 完成：

- 输入为 92,060,493 字节、900.899983 秒的 H.264/AAC MP4，稳定 BS URL 位于
  `klingcorp-agentk-engine/narrato-agentk/uploads/`；Narrato 使用 31 个视觉窗口，显式模型
  `gemini-3.1-flash-lite`。所有窗口都先裁为 AgentK BS 产物，再通过 KCDN URL 理解，未走
  base64；脚本依据中包含《钢铁侠》车队、袭击、洞穴、赌场、杰里科导弹等真实画面。
- 首次最终合成因 AgentK Worker 的 `PATH` 选中了不含 libass 的普通 Homebrew FFmpeg，
  `subtitles=true` 明确失败于 `ffmpeg subtitles filter is unavailable`。把
  `/opt/homebrew/opt/ffmpeg-full/bin` 放到 AgentK Worker 的 `PATH` 最前并重启后，复用
  已生成脚本原生重跑 `documentary_render` 成功，不需要关闭字幕。
- 字幕版 execution 为 `exe_01a04e09-3e7e-7509-9f4a-131489a58298`，产物稳定 BS URL 为
  `https://bs3-hb1.staging.kuaishou.com/klingcorp-agentk-engine/capabilities/videos/2c93ccbe72dd38e82a1d009638005c57.mp4`。
  6 个 compose batch 全部携带字幕片段，最终 concat 成功；下载校验为 112,712,578 字节、
  902.044 秒、1280×720 H.264/AAC。在 2、422、752 秒抽帧均能看到中文硬字幕，证明不是
  只检查了接口状态。成片包含 23 段中文旁白。
- 普通助手 session `ses_002acf2e51d918ea3e1bdb45` 显式选择
  `gemini-3.1-flash-lite`，以最终成片 420–450 秒的受管 BS 片段作为原生视频附件，0 次工具
  调用即正确识别托尼·斯塔克、女记者、夜间采访，并听出新增中文旁白及其军火商身份、
  和平哲学与矛盾主题。整片 116 MB 超过当前 Kigress 的 15 MiB 拉取上限，因此助手验收
  使用受管片段，不把整片改成 base64。

case profile 是从 `comic-drama` 的用途和工具组合整理出的测试版，不依赖 Go API
Server；如生产 profile 发生变化，应同步更新 `src/agentk_capability_demo/catalog.py`。

### Agent 100 / Narrato

第三个顶层 tab 是可扩展的 Agent 100 测试区。每个 Agent 拥有独立的配置、API、
契约、Workflow 和 Worker；当前第一个模块是 Narrato，包含三个 Pipeline：

- `documentary_video`：从源视频一键生成解说成片。等价于依次执行下面两条，
  中间那份脚本由它自己接。
- `documentary_script`：只生成可编辑的结构化脚本。
- `documentary_render`：使用人工修改后的脚本重新配音，并在完整原片上叠加与合成。

前两条支持 `style` 参数选择解说风格，取值定义在
`agent100/narrato/contracts/styles.py`：

- `documentary`（默认）：克制、有画面感的伴随旁白。避让原片对白，严格按事件
  后置安排解说，覆盖率 55%-70%，原声保留在 0.2。适合纪录片、Vlog、实拍素材。
- `recap`：全程复述剧情的影视解说。盖着原片对白讲，主动交代人物与因果，
  覆盖率 88%-96%，语速 4-5 字/秒，原声压到 0.08。适合影视剧、预告片、番剧。

风格决定的不只是文案语气，还包括是否把原片对白计入「已覆盖」——`documentary`
计入，所以原片有人说话的区间不会强求补解说；`recap` 不计入，否则连续对白的
素材几乎不会产生任何解说。新增风格只需在 `styles.py` 里加一个预设。

Narrato 的 Worker 已并入本项目。三条 Pipeline 由同一个 Narrato Worker 承载，
不会分别启动三个 Worker。Web（Uvicorn）是 HTTP 服务进程，不是 Worker。复制示例
配置并填写模型、Temporal、媒体存储和 CDN：

```bash
cp config.narrato.example.yaml config.narrato.yaml
export NARRATO_CONFIG="$PWD/config.narrato.yaml"
```

本地联调时，`agentk.base_url` 与 Web 的 `--agentk-address` 必须指向同一个 AgentK API，
`temporal.address` 必须指向 AgentK Core Worker 使用的同一个 Temporal。standalone 会在
启动日志中输出实际 Temporal 端口；本次 E2E 使用 namespace `agentk`。

不使用泳道时，一个 Narrato Worker 即可。使用个人泳道时，全局 ASR limiter 仍固定在
raw `demo-worker`，业务 Workflow 才由 SDK 路由到 `demo-worker@<lane>`，因此必须同时启动
baseline 和 lane 两个 Worker。两个进程都填写 base queue `demo-worker`，不要手工拼
`@<lane>`：

```bash
# 终端 1：baseline，承载 namespace-wide ASR limiter
env -u KWS_LANE_ID \
KWS_SERVICE_STAGE=STAGING \
KWS_SERVICE_NAME=klingai-agentk-capability-demo \
NARRATO_TEMPORAL_ADDRESS=127.0.0.1:<standalone-temporal-port> \
NARRATO_TEMPORAL_NAMESPACE=agentk \
NARRATO_WORKER_TASK_QUEUE=demo-worker \
NARRATO_CORE_TASK_QUEUE=agentk \
uv run agentk-capability-demo-worker narrato

# 终端 2：个人泳道业务 Worker；SDK 自动 poll demo-worker@dev_yangshuo
KWS_LANE_ID=dev_yangshuo \
KWS_SERVICE_STAGE=STAGING \
KWS_SERVICE_NAME=klingai-agentk-capability-demo \
NARRATO_TEMPORAL_ADDRESS=127.0.0.1:<standalone-temporal-port> \
NARRATO_TEMPORAL_NAMESPACE=agentk \
NARRATO_WORKER_TASK_QUEUE=demo-worker \
NARRATO_CORE_TASK_QUEUE=agentk \
uv run agentk-capability-demo-worker narrato

# 终端 3：统一的三 tab 浏览器测试台
KWS_LANE_ID=dev_yangshuo \
FALLBACK_TO_ENV_LANE_ID=true \
KWS_SERVICE_STAGE=STAGING \
KWS_SERVICE_NAME=klingai-agentk-capability-demo \
uv run agentk-capability-demo \
  --agentk-address http://127.0.0.1:8081
```

`FALLBACK_TO_ENV_LANE_ID=true` 只用于本地没有 ingress `trace-context` 的请求；公司部署
应优先沿用入口真实携带的 lane，不用环境变量替它染色。

烧录字幕的 `compose_video` 运行在 AgentK Core 进程。macOS 默认 ffmpeg 如果没有
`subtitles`/`libass` filter，会在最后合成阶段失败；启动 AgentK 前应把
`/opt/homebrew/opt/ffmpeg-full/bin` 放在 `PATH` 最前，并用 `ffmpeg -filters` 确认存在
`subtitles`。只给 Narrato Worker 改 PATH 没用。若直接执行 FFmpeg 时中文字体回退失败，
还应显式选择系统中可访问的 CJK 字体；AgentK 生产 Worker 镜像已在构建阶段验证 Noto
Sans CJK 字体选择和中文 SRT 渲染，但线上仍需确认部署的是包含该门禁的镜像版本。

容器部署直接使用镜像内的 `/start.sh`：它启动一个 Narrato Worker 和一个 Uvicorn
进程，并统一管理两者的退出与重启。AgentK Core Worker 属于外部 AgentK 基础设施，
不由 capability-demo 镜像启动。

Narrato 与通用文件上传共用 `AGENTK_DEMO_BLOB_ENDPOINT` 和
`AGENTK_DEMO_BLOB_BUCKET`，不需要额外配置 `NARRATO_UPLOAD_BUCKET`、
`NARRATO_BS3_ENDPOINT` 或 `NARRATO_CDN_BASE`。上传地址和公开读取前缀统一使用
`<endpoint>/<bucket>`。Narrato 仍保留内容哈希去重、512 MiB 文件上限和 1 小时时长
上限；浏览器会在上传前读取本地视频 metadata 提前提示，服务端仍以 `probe_video`
结果执行不可绕过的时长校验。浏览器只接收业务配置，AgentK API key 仍保存在 Python
进程中。

长视频按窗口处理 ASR 和视觉证据。ASR 默认自动检测语言，单次尝试最多 45 秒并使用
15/30/60 秒递增退避；重试耗尽或响应畸形时只降级当前窗口。受管 BS 视频的正常链路是
URL-first；HTTP 413 绝不会触发 base64。Narrato 若收到明确的源文件过大错误，会用
`process_video` 物理裁出当前窗口并写回 BS，再通过新 CDN URL 理解；这不是 inline
fallback，也不改变其他 capability 的传输策略。内容生成只有
“整片叙事策划”和“带插入区间的具体脚本”两个正常 LLM 阶段；题材由策划自动判断，
一段解说可以跨多个镜头和多段 ASR。策划仅在确有需要时请求联网检索，多条查询中的
单条失败不会终止脚本。视频理解会给关键视觉事实标注原片时间，ASR 保留分段起止时间；
自动脚本的每个 cue 会尽量记录 `evidenceEndSeconds`，软检查建议
`startSeconds >= evidenceEndSeconds`，让人物行动、对白、身份、结果和转折默认都在发生后再解说。
开场 hook 同样只应使用播放前已经出现的证据，不应为了 3 秒开场或 5 秒空档提前剧透。软检查
发现缺少证据时间、解说早于证据或引用窗口不一致时，会把问题清单和上一轮完整脚本一起交给
模型，最多修订两次；多次仍未改善则直接采用最后一版并携带 `pacingWarning` 继续，不因创作时间
判断终止 Pipeline。TTS 最终排布也不把 `evidenceEndSeconds` 当作硬门禁。

渲染输入统一使用 `tts.provider/model/voiceId`。如果 AgentK capability schema 发布了
`qwen3-tts-instruct-flash`，页面会优先选择它，并把策划的全局声音要求与每段
`ttsInstruction` 合并传入 AgentK Qwen 的 `instructions`；如果只发布普通
`qwen3-tts-flash`，渲染会自动省略不受支持的 `instructions`。MiniMax 支持脚本逐段生成的
`emotion`。最终
成片连续保留原视频 0 秒到片尾的画面和总时长，只在解说区间压低原声并叠加 TTS。

Pipeline 提交是异步的：Web 收到 AgentK 返回的 `sessionId`、`executionId` 和初始
`status` 后立即响应，页面每 10 秒查询一次执行状态。Demo 在 Web 服务进程内保留最近
50 次执行快照；刷新或离开页面后仍可从 `RECENT RUNS` 重新打开，容器重启后清空。

迁入的 Narrato AgentK 代码及其对 NarratoAI 行为/提示词设计的归属见
[`NOTICE.narrato-agentk`](NOTICE.narrato-agentk) 与
[`LICENSE.narrato-agentk`](LICENSE.narrato-agentk)。

## 验证

```bash
uv run ruff check src tests
uv run pytest -q
node --check src/agentk_capability_demo/static/app.js
node --check src/agentk_capability_demo/static/narrato.js
```

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/api/connection
```

## AgentK API 多泳道

入口染色、SDK 依赖切换和 PRT 验收见 [部署说明](deploy/lanes.md)。
