# Video Judge Demo

基于 VLM 的生成视频质量评测：把评测拆成**可审计的多阶段**，而不是让模型直接给一个黑盒总分。程序负责确定性的文件检查、抽帧、加权与拦截；模型负责看画面、拆提示词、逐条裁决。

服务只做一件事：**给一条生成视频出质检报告**。输入是浏览器上传（或从离线数据集里挑一条现成素材）的视频 + Prompt + 参考图 / 参考音频 / 参考视频，输出是可审计的分维度报告；一次选中同一 case 下的多条视频时，会在单条质检之上再聚合一份横向排名（listwise）。

## 启动

```bash
./deploy/run_agentk.sh                    # 默认通道，自动挑 3.12+ 解释器
PYTHONPATH=src python3 -m aigc_eval       # 不走脚本时的等价写法
```

打开 http://127.0.0.1:9900 （端口可用 `VIDEO_JUDGE_PORT` 覆盖）。

- 依赖：系统 `ffmpeg` / `ffprobe`（需支持 psnr/ssim/blackdetect/freezedetect/tblend/signalstats）。
- **主链路零第三方 Python 包**，纯标准库；`pyproject.toml` 的 `dependencies` 是空的。唯一例外是离线批跑脚本读 xlsx 标注表要 pandas，放在可选依赖 `[xlsx]` 里，服务本身不碰。
- 服务监听 `0.0.0.0` 且**无鉴权**，仅适合可信内网。

### 模型通道（provider）

只有一条：`agentk`，走平台的 `understand_media` / `generate_text` 两个能力。`./deploy/run_agentk.sh` 启动。`config.LIVE_PROVIDERS` 是唯一真源，健康检查、
报告渲染、前端下拉、批处理参数校验都读它。

曾经并存过另两条，都已删除：

| 已下线 | 是什么 | 为什么删 |
|---|---|---|
| `keye` | 本机 / 内网 Keye、vLLM，OpenAI 兼容 | 判定早已固定走 agentk，只剩维护成本 |
| `gemini` | 直连 Gemini（KLink 网关或 Vertex） | 见下 |

删 `gemini` 的判断依据是一条容易被忽略的事实：**`understand_media` 底层就是
Gemini**——它的 schema 里 `model` 只接受 `gemini-3.1-pro-preview` /
`gemini-3-flash-preview` / `gemini-3.1-flash-lite`。所以直连不是"换一个更强的模型"，
只是"同一个模型的另一条传输"，代价是自己维护一套凭据、重试、JSON 修复。删除前
它已零流量（近百次 run 的 trace 里 `engine` 全是 `agentk`）。

它唯一带来过的真实能力差异是音频听感——而这条差异后来被证明比想象的小得多：视频
音轨本来就能进 `understand_media`（见下面的「音轨其实进得去」），真正送不进去的只有
独立音频文件。剩下的那点缺口在删除**之前就已经存在**（判定早已走 agentk），删除失去
的不是能力，而是"想自己接音频时不用重写"的退路。正确的补法是让平台把音频口子开出来，
而不是重建直连。

解释器由 `deploy/find_python.sh` 自动发现 3.12+ 版本，也可以 `PY=/path/to/python3.12 ./deploy/run_agentk.sh` 显式指定。

### 目录结构

```
src/aigc_eval/        判定代码全部在包内，按角色分层
├── paths.py          全项目唯一的路径锚点（见文件头注释）
├── config.py         凭证与 provider 配置
├── history.py        历史质检记录（对 runtime/results 的只读投影）
├── server/           HTTP 服务与作业轮询
├── providers/        传输层：agentk / blob_store / media_io / json_parse
├── judge/            判定核心：pipeline、rubric、scoring、evidence、integrity
├── report/           HTML 报告与人评对比
├── dataset/          数据集缓存、上传 case
├── batch/            离线批跑与 F1 评测（CLI，不经 HTTP）
└── static/           前端：单页 studio.html + studio/ 控制器 + shared/ 基建
worker/               作为 AgentK capability 被调用的那一侧
├── abi.py            对外契约：ID、队列、workflow/activity 名、输入输出模型
├── workflow.py       编排：八个阶段依次起 activity，转发进度
├── stage_activities.py  八个阶段 activity（阻塞动作都在这里）
├── state.py          阶段状态在 activity 之间的传递
└── verify_agentk.py  真机联调脚本（pipeline / tool 两种模式）
deploy/               启动脚本
docs/                 设计文档
runtime/              运行时产物（gitignore，不进包也不进镜像）
```

包外的运行时产物（`runtime/`）以**仓库根**为锚点，数据集目录（`I2VA*` / `T2VA*` / `mm-datasets`）按约定放在**仓库的上一级**。这三个层级都在 `paths.py` 里算一次，其余模块一律从那里取——不要再写 `Path(__file__).parent / "runtime"`，文件换层级时那种推算会静默指向错误目录。

## AgentK 接入

本项目作为 **AgentK Contrib 的独立 API 服务**接入，位于 `Contrib/aigc-eval/`，与 `Contrib/comic-drama/` 平行。

### 形态：双向

AgentK 的 Contrib 有两种形态——注册 Temporal 能力的 Worker（如 `ffmpeg-worker`），和通过 API 消费 AgentK 的下游应用（如 `comic-drama` 的默认 agent 模式）。**本项目两边都占**：

- **作为调用方**：HTTP 服务把 AgentK 当模型后端，直调 `understand_media` / `generate_text`（本章其余部分讲这一侧）。
- **作为被调方**：`worker/` 注册 `contrib.aigc_eval.video_judge`，让别人的 agent 能把质检当工具或流水线调用（见「作为 Worker 被调用」章节）。

之所以必须是 Worker 而不能直接把 HTTP 服务挂上去：`Profile.tools[]` 只能引用 capability ID，而 capability 只有 Temporal 承载的 WORKFLOW / ACTIVITY 两种类型（`agentk-sdk/protocol/capability/v1/manifest.proto`），平台既不支持 MCP、也没有 HTTP 工具注册端点。

### 调用链

每次模型调用是一次 `InvokeCapability` 直调（SDK 0.3.2 起的同步阻塞式接口），**绕开 agent 的 LLM 决策循环**，行为确定且无额外模型开销：

```
InvokeCapability(capability_id, input)   # 一个 RPC，等它跑完直接拿输出
```

早先这里是 `CreateSession` → `StartExecution` → 轮询 `GetExecution` 三段式。换掉它不只是省两个 RPC：那套流程还必须为平台的「一个会话同时只能有一个活跃执行」限制维护一个按线程隔离的会话池，因为 `judge/pipeline.py` 用 `ThreadPoolExecutor` 并发跑扫描分片，共用单会话会让除第一个分片外的调用全部撞 `failed_precondition`。直调没有会话概念，这个约束和那段代码一起消失了。

传输走官方 Client SDK，**按环境选通道**（`providers/agentk_transport.py`，规则在 `config.default_agentk_transport`）：生产走 KESS gRPC，staging 与本机走 HTTP 固定网关。SDK 只提供 `AsyncClient`，而质检链路是同步的且跑在线程池里，所以那一层用**一条常驻事件循环线程**做 async→sync 桥，所有工作线程共用一份连接池。

为什么必须分环境：生产的 HTTP 固定网关（`agentk-api.internal`）不对我们开放。这一条在 prod 容器里用同一份身份、同一个进程做过 A/B——HTTP 回 `unauthorized`，KESS gRPC 正常回身份。**两条通道失败时的错误一模一样**，与「服务名没登记」无法区分，所以排查时别只盯一条，用 `deploy/container/whoami_probe.py` 并排跑一次就能定性。staging 保持 HTTP 是因为 KESS 发现依赖运行环境的 kess-agent，开发机上未必有。

**HTTP 通道有一条硬上限：网关对同步调用的容忍是 300 秒**，而 `understand_media` 自身的 capability 超时是 360 秒——300~360 秒之间存在一个区间，能力还在跑、同步调用已被网关切断（实测：给一个不可达的媒体 URL，恰好 300.0s 后回 `unavailable / Gateway Timeout`）。真实观测到的最长单次调用是 4b 主判约 137s，有两倍余量，但更长的视频有可能逼近上限，所以传输层会把这种失败单独措辞，别让它看起来像一次普通的服务抖动。gRPC 通道直连实例、没有网关那一跳，这条上限**大概率不适用**，但尚未实测——排查生产超时时别照搬这个模型。

**这条 300 秒上限只作用在 HTTP 路径上**（批处理脚本）。worker 路径已经改走 Temporal 原生的 `invoke_capability`，不经网关，天花板是平台给 `understand_media` 的 360 秒（见「模型调用：worker 走 Temporal，不走网关」）。所以别把它当成全局边界——两条路径的上限不一样。studio 已不在这条路径上（见「studio 走 capability」）。

### 模型调用：worker 走 Temporal，不走网关

两条路径的模型调用是同一份代码、不同的最后一跳：

| | 批处理脚本 | worker |
| --- | --- | --- |
| 谁发起 | activity 内部同步 HTTP（`HttpInvoker`） | workflow 的 `invoke_capability`（子 workflow） |
| 上限 | 网关 300 秒 | 平台能力 360 秒 |
| 结果落在哪 | 只在进程内存里 | **workflow 历史里** |
| 能否真取消 | 不能，HTTP 已经发出去了 | 能，`WAIT_CANCELLATION_COMPLETED` |

走 Temporal 换来的实质好处只有一条，但很值：**模型响应进 workflow 历史**。阶段因实例丢失重跑时，已经付过钱的调用直接从历史里取，不会重发。这条性质由三处钉住，见「崩溃与重启」一节的实测记录。

### studio 走 capability

studio 不再在自己进程里跑 `analyze_case`，而是作为 `video_judge` capability 的调用方：上传的素材先进对象存储换成 URL，再经 `start_execution` 发起、`watch_events` 收进度，最后从 `reportJsonUrl` 取回完整报告（`server/capability_job.py`）。

这么做是为了让判据在全公司只有一份。此前两条路径是两套独立实现——同一份判据，一边经 Temporal 编排、调用进历史、可取消、崩溃能续跑，另一边什么都没有；两边只要漂一处，同一条视频在 studio 和在 capability 上就会给出不同结论，而且没有任何地方会报错。

代价是 studio 失去了两个没进对外契约的调试开关（`use_vlm`、`allow_fallback`）。它们本来只对本地调试有意义，不该出现在别人也要调的能力签名里。

有三处约定横跨 studio 与 worker，没有编译期检查兜底，`tests/test_studio_capability_job.py` 逐条钉住：

- **阶段事件的压扁与还原**。worker 的 `_stage_progress` 把事件压成 `(step_id, message, data)`，studio 的 `event_from_progress` 拆回来。改了一边不会报错，表现是界面上阶段行的标题或状态静默丢失。
- **模型调用进度要丢掉**。workflow 每次发起模型调用会补一条 `<阶段>.model.<第几次>`。它只有 `running`、永远没有 `done`，而前端按 id 建行——收下它们就是几行永久停在「运行中」的假阶段。也不能并进父阶段行：step_id 里是 workflow 的阶段键（`scan`、`evidence`），跟界面上的阶段 id（`unified_scan`、`grounding`）本来就对不上。
- **报告里的素材地址**。报告是 worker 生成的，`media` 里的 slug 是 worker 算的；studio 要换成自己的，否则页面上的视频是死链。slug 由内容派生、两边通常一致，但参考素材的标注只有 studio 有，归一化结果可能不同——与其赌它相等，不如显式重写。

**本机联调必须给 studio 一个泳道**：worker 以 `contrib.aigc-eval@dev/<用户名>` 注册，studio 不带 `KWS_LANE_ID` 的请求走基线队列，落不到它上面。表现是执行一直排队、没有任何进度——看着像 worker 没起来。

```bash
export KWS_LANE_ID="dev/$(id -un)"     # 与 deploy/run_worker.sh 默认的泳道一致
bash deploy/run_agentk.sh
```

容器部署相反：worker 是基线身份，studio 也不要设这个变量。

（超时那 60 秒的差距是顺带的，不是理由。改造前后都需要 `judge/model_checkpoint.py` 那层跨 run 检查点，因为 workflow 历史只在一次 run 内有效。）

**判定代码一行没改**，靠的是切点选在 provider 的 `generate_json` 这一层，而不是七个业务调用点：

```
build   （activity）组装素材、上传对象存储、拼提示词  → ModelRequest
invoke  （workflow）invoke_capability                → ModelResponse
consume （activity）解析、记 usage/trace              → 判定要用的 dict
```

`invoke` 必须在 workflow 里（`invoke_capability` 底层是 `execute_child_workflow`，只能在 workflow 上下文 await），而 `build` / `consume` 必须在 activity 里（要读文件、要发上传请求）。activity 又没法阻塞等 workflow 把结果送回来——Temporal 没有 workflow → 运行中 activity 的数据通道。所以三段式不是设计取向，是这个形态在 Temporal 里唯一可能的切法。

三段之间怎么串起来，是这次实现里唯一有选择的地方，选的是**重放**（`judge/model_relay.py`）：

> 阶段函数原样跑，跑到需要模型时抛 `NeedsModelCall` 把请求带出去，拿到响应后从阶段开头重跑一遍。

于是 workflow 侧只需要一个通用循环就能驱动任意数量、任意条件依赖的调用——4c 那三次（疑点复核、音频复听、音频反证，第三次只在第二次判出 violated 时才发生）不需要 workflow 知道它们的依赖关系。activity 定义还是八个，不是十八个。

代价是**每次调用之前的确定性工作会重算一遍**（组装 parts、编码帧、算 digest）。所以只有含模型调用的五个阶段可重入；`prepare` 不可以——它是唯一可能跑到二十分钟的阶段（下载、ffprobe、逐时刻 SSIM、抽帧），重放它是纯亏。这条分工写在 `abi.MODEL_STAGES`，测试与 workflow 共用同一份。

重放要求阶段函数可重入，这一点是核过的而不是假设的：`judge/` 下没有任何 `random` / `time.time` / `uuid` / 临时文件名依赖。副作用三类都可重复——磁盘产物按内容与时间点定名、对象存储内容寻址、而 usage / trace **只有跑到阶段末尾的那一遍才会被写进状态**，中间几遍随 client 实例一起丢弃，所以最终记下来的恰好是完整一份，不重不漏。

唯一需要额外处理的是**进度事件**：中间几遍会重复 emit，由 workflow 按 `stepId` 去重（平台本身也用 step_id 做幂等），因此调用方看到的事件序列与改造前逐条一致。

### 配置

| 变量 | 说明 |
|---|---|
| `KWS_SERVICE_NAME` | 天琴服务名，**必填且是唯一必填项**（本项目为 `klingai-agentk-aigc-eval-api`） |
| `KWS_SERVICE_CREDENTIAL` | 可选，平台注入时一并透传 |
| `KWS_SERVICE_STAGE` | 决定默认网关：`STAGING`/`CANDIDATE` → staging，其余 → 生产内网 |
| `AGENTK_BASE_URL` | 可选，显式指定则覆盖上面推导出的网关 |
| `AGENTK_UNDERSTAND_MODEL` | 默认 `gemini-3.1-pro-preview`，须落在 `understand_media` 的 enum 内 |
| `AGENTK_CHAT_MODEL` | 默认 `deepseek-v4-pro`。`generate_text` 的执行模型，必须是 chat 家族——填多模态模型会被拒（实测 `gemini-3-flash-preview` 报 `text capability not available`） |
| `AGENTK_MAX_MEDIA` | 默认 10，即 `understand_media` 的 `mediaUrls` 上限 |
| `AGENTK_TIMEOUT` | 客户端侧等待上限，默认 420s。真正卡死同步调用的是网关的 300s，给得比它大也不会让调用活得更久 |
| `AIGC_EVAL_BLOB_ENDPOINT` | 对象存储 endpoint，**必填**；staging 为 `https://bs3-hb1.staging.kuaishou.com` |
| `AIGC_EVAL_BLOB_BUCKET` | 对象存储 bucket，**必填**；业务共享桶名向平台侧索取 |
| `AIGC_EVAL_BLOB_CDN_BASE` | 可选，配了就返回 CDN 直链 |

`understand_media` 只接受 HTTP URL，所以本地视频和抽出的帧必须先上传（`providers/blob_store.py`，协议与 comic-drama 的共享上传一致：匿名 `PUT {endpoint}/{bucket}/{key}`）。

**不需要公网 CDN，也不需要与 AgentK 服务端的 `AGENTK_BLOB_*` 对齐。** `understand_media` 对认不出的 URL 按外部直链处理，由 AgentK 自己下载再内联给模型（`mediaunderstand.go` 的 `fetcher.one` → `inlineFromURL`，该路径不校验内网地址）。所以唯一的要求是 URL 从 **AgentK 服务端**可达——内网对象存储域名正好满足。对齐 `AGENTK_BLOB_*` 只影响 URL 能否被认成受管 artifact，从而走「上游自取直链」的优化路径；而该优化对本通道没有实际收益。

上传是**内容寻址**的：key 由字节的 sha256 生成，进程内还有一层 URL 备忘缓存。一次质检要对同一段视频发起 8~9 次模型调用，没有这层去重整个流程会被上传拖垮。

配置是否齐全直接看 `GET /api/health` 的 `providers.agentk`，它会分别报告 `api_configured`、`blob_configured` 与当前生效的 `service_name`（凭据不外露）。

### 鉴权：天琴服务名

AgentK 从 SDK 0.3.0 起不再用 API Key（官方 Client SDK 也不再支持发 Bearer），改由调用方带上自己的服务身份，平台按服务名授权。**本地连 staging 只需要这一个变量**：

```bash
export KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api
```

身份头由 SDK 在建客户端时读环境变量生成（`kws-bin`，外加可选的 `kwsCredential`），所以它只认环境变量、不认配置文件——`AgentKConfig.service_name` 也是从同一个环境变量读的，只用来做"配置齐不齐"的前置校验。

验证方式：`WhoAmI` 会回平台认到的身份。本项目的服务名对应 `display_name='质检 API'`，一旦头编错就会退化成匿名调用，而那种错误只有连上 staging 才暴露得出来。`providers/kws_identity.py` 是同一套编码的独立实现，由 `tests/test_kws_identity.py` 用 SDK 自带的**跨语言基准向量**锁定；SDK 已经内置这件事之后它不再参与调用链，留着是作为编码基准。

开发机上不需要 `kess-agent`：本机连的是 staging，走 HTTP transport 按 `KWS_SERVICE_STAGE` 推导网关，什么都不用装。**生产反过来必须有它**——那边只有 KESS gRPC 通，容器云已内置。（**worker 侧则两个环境都要**：Temporal 地址硬走 KESS 发现 `klingai-muse-temporal-server`，SDK 没留环境变量后门，见下文 Worker 章节。）

本地调试时把这几个变量收进 `secrets/agentk.sh`（`secrets/` 已在 `.gitignore` 内），每个新终端 `source secrets/agentk.sh` 即可。

### 错误分流

失败按 **Connect 错误码**分流，而不是匹配错误信息措辞：

| 错误码 | 含义 | 是否重试 |
|---|---|---|
| `unavailable` | 网关切断（含 300s 同步上限）、服务不可用 | 是 |
| `deadline_exceeded` / `resource_exhausted` / `aborted` | 超时、限流、冲突 | 是 |
| `unknown` | **capability 自身的业务错误**（模型不可用、参数不合法） | 否 |
| 其余 4xx 类 | 参数/权限/未实现 | 否 |

`unknown` 不重试是刻意的：平台把能力内部的业务错误都塞进这个码，重试只会把同一份素材再烧一遍。判定逻辑在 `AgentKCallError.retryable` 上，不在调用方的字符串匹配里——原先靠 `"HTTP 429" in message` 这类措辞分流，换一次传输层就会静默失效。

### AgentK 通道的能力边界

这条通道实际用到平台的**三个能力**，按素材类型自动分流：

| 调用类型 | 能力 | 模型 | 典型环节 |
|---|---|---|---|
| 带素材 | `agentk.understand_media` | `AGENTK_UNDERSTAND_MODEL` | 看视频、维度扫描、逐条核对 |
| 纯文本 | `agentk.generate_text` | `AGENTK_CHAT_MODEL` | 拆解提示词要求、汇总评分 |

分流是必需的而非优化：`understand_media` 的 `mediaUrls` 是 `minItems=1`，纯文本环节送不进去。两个能力的输出形状不同（`{text, model, params}` 与 `{result}`），由 `raw_text_from_output` 按能力 id 收敛。

曾经还有第三行 `agentk.transcribe_audio`（把参考音频与视频音轨转成带时间轴的文字）。2026-09-03 下线，见下。

### 音频：全部交给 `understand_media`，不再有转写

现在只有两条路，取决于这次调用派到哪个模型：**视频自带的音轨**随 mp4 一起进模型（一直如此，见下面「音轨其实进得去」）；**独立的参考音频**只在走 `audio_model` 的阶段进 `mediaUrls`，其余阶段送不进去、只在提示词里标明「本次听不到」。

#### 转写为什么下线（2026-09-03）

原先还有第三条：参考音频与视频音轨都先经 `agentk.transcribe_audio`（azure-whisper）转成带时间轴的文字随 prompt 送入。它提供两样额外的东西——精确到句的时间轴，以及一份可对照的文字。下线有两个理由，第一个是被生产环境逼出来的。

**它在 prod 挂着不返回。** 请求被接了、连接一直开着，直到我们自己 120 秒后取消。而失败结果没法缓存（缓存键是内容哈希，只存成功值），worker 路径上每一遍重放都要重付一次满额超时。一次带参考音频的质检里有两个音频源、observe 阶段里有两次模型调用，4 × 120 = 480 秒，顶穿了该阶段 420 秒的 activity 预算，**整条质检失败**。

这个塌法值得记住：一个旁路证据把主流程拖死了。而且它在本地单体路径上永远看不见——那条路没有重放，代价不会相乘。发现它靠的是在 prod 上真跑一条带参考音频的用例（`verify_agentk.py --reference-audio-url`）。

**第二个理由是它的价值已经被 flash-lite 吃掉大半。** 音频判据现在集中在 4a（参考音频对照）与 4c（音频独立复听）两个阶段，它们都跑在真听得见的模型上，能给出转写给不了的音色、语调、音质判断。

**代价要说清楚：没有精确时间轴了。** 模型自报的时间点偏差约 1 秒，所以"第 3.2 秒那声爆音"这种精度的结论不再可靠，音画同步只能给到"大致对齐 / 明显不同步"这一档。注入措辞里明写了「不要谎报精确到秒」，否则模型会照着习惯报一个看起来精确、其实编出来的时间点。

#### 音轨其实进得去（2026-09-01 实测，推翻了原先的结论）

本文档此前断言这三条子准则「永久无法核实」：

    audio_signal_quality        有无爆音、底噪、削波
    audio_av_sync               口型与声音是否对齐
    audio_temporal_consistency  音量/音色跨镜头是否连贯

**这个断言的前提是错的。** 它推理成"schema 不收音频 → 音频进不了模型"，漏掉了 mp4 本身带音轨、而平台是把视频 URL 透传给 Gemini（原生支持带声音的视频）这条路。

`scripts/probe_audio_passthrough.py` 用视听解耦的方式验了：画面用 testsrc2 测试图案（没有人、没有口型、没有字幕，读唇与 OCR 都拿不到东西），音轨换成自己合成的一句胡话加若干纯音调，提示词里**不注入任何转写文本**、也绕开整条 judge 流水线。结果模型把那句胡话逐字复述了出来，三轮全中——其中「香蕉**牌**」这个字 `transcribe_audio` 反而听错成「香蕉**排**」，而提示词里根本没有转写文本，所以这条只能来自模型自己听。

**但精度有限，别过度外推。** 同样三轮里，纯音调的数量分别答 2 / 3 / 4（真值都是 3），时间偏差约 1 秒；真值经 `ffmpeg silencedetect` 客观核对过，确认是模型答错而非探针做错。所以能下的结论是：

- **音频确实进得去，人声内容可判** —— 这一点确凿；
- **要求精确定位或计数的判据仍需各自验证** —— 逐帧口型对齐、爆音计数落在这一类；
- 模型会自发做跨模态推理：有一轮它未经提示就注意到"画面中人物口型在动，但无语音"，那正是 `audio_av_sync` 要的推理。

改口径之前请先重跑那个探针：平台行为会变，这个结论是有时效的。

#### 独立音频文件：套一层视频就能进（已验证可行，但决定不用）

这一段是 2026-09-01 平台放开音频入口**之前**的探索，结论仍然有效但已经不需要了。当时 `mediaUrls` 不收 `.mp3` / `.wav` / `.m4a`，而既然平台收 mp4、mp4 的音轨又进得去，给音频套一层静态画面就能绕过这个 schema 限制。

`scripts/probe_audio_passthrough.py --wrap-audio <文件>` 验过了，用的是真实参考音频素材（`reference_audio_01.mp3`，229KB / 14.2s，套黑底视频后 137KB）。模型不但把台词听全了，还给出了转写根本给不了的判断：

- **语调**：认出是女声播音腔，并指出"语调平稳生硬、缺乏情感起伏、带有极其明显的 TTS 合成特征"；
- **配乐**：判定无背景音乐，是纯人声干音；
- **音质**：指出"明显的数字压缩失真、电子感与金属音色、低比特率特有的毛刺感"，同时确认没有爆音与削波。

最后一条正是 `audio_signal_quality` 想要的结论。顺带一个附带发现：模型的听辨比 whisper 准——转写把「银发诵初心，新声贺华诞」听成了「銀髮宋初心新生賀華旦」（无意义），把「万柏林区」听成「萬博林區」，模型都听对了。它还主动标注了哪几个词是按发音推测的同音词。

**这条路通，但不必走了**：平台已经放开原生音频入口（见下），套视频这种绕开 schema 校验的做法没有契约保证，`--wrap-audio` 因此停在探针脚本里作复核工具，不接进判定链路。

留一条当时反对接入的实测证据，因为它现在变成了能力边界：**音质结论跨轮不稳定。** 同一个参考音频文件跑两轮，一轮说"音质较差、明显的数字压缩失真、金属音色、低比特率毛刺感"，另一轮说"非常清晰干净，没有明显底噪、爆音、削波或压缩失真"。语调那项两轮都指向 TTS 合成，但强度从"极其明显"弱化成"有些像"。方向一致、强度飘。

#### 原生音频入口：能用，但**换哪个模型**是决定变量

2026-09-01 16:05 平台放开 `mediaUrls` 收音频。第一批实测让人以为它不可用，后来发现是模型选错了。

**主模型（`gemini-3.1-pro-preview`）上只有约一半的调用真的走原生。** 当天实测 28 次直送（16:10 前后 8 次，17:05 前后 20 次），累计 **15 次拿到原生音频、13 次只拿到服务端转写的文字**——同一个文件、同一段提示词，结果在两种行为间来回跳，且发版后一小时比例没有改善。

不像是"滚动发版没滚完"那么简单：20 次那轮里结果**成簇出现**（第 3~8 次连续原生，第 16~20 次连续文字），更像是实例级粘连——同一个连接落到哪台实例就一直是那台的行为。

判别器用的是带非语音声音的合成音频：一句台词加 3 声 1000Hz 纯音调（7.0/9.5/12.0s）。纯音调不是语音、不会出现在任何转写里，所以能干净地区分两种情况：

- **拿到原生音频那几次**：模型答出"人声结束后有三声明显的电子提示音效"，数量和位置都对，还判了 TTS 合成腔、语速、音质。这是真听到了。
- **只拿到文字那几次**：模型精确复述了台词，却对哔声只字未提，并明说"仅接收到了文本输入（包含时间戳和文字内容），未接收到实际的音频文件或音频信号"。而 `--raw-audio` 的提示词里**没有任何转写文本**，所以那段台词只能来自平台注入。

后一种状态最容易被误判成"已支持"：接口通、台词对、还带时间戳，接入层面完全看不出问题，但音色、语调、音质、配乐一概拿不到，报告也不会有任何异常。放开前至少是响亮的失败（报 "cannot determine a supported media type"，虽然要等 315~346s，因为平台把它标成可重试）。

所以两条使用纪律：探针的判定既不看有没有抛异常、也不看台词对不对，而是看模型自己答的 `can_hear_audio` 加上"有没有答出非语音的声音"；并且**必须连跑数次**，跑一次会因为实例差异得出相反结论——我们就先后得出过两次相反的结论。

**`gemini-3.1-flash-lite` 上 8/8 都是原生。** 18:15 前后连跑 8 次同一份判别器音频，全部 `can_hear_audio: true`，且都给出了转写给不了的判断（判出 TTS 合成腔、说了语速语调、评了音质）。所以音频判据不是判不了，是**不能用主模型判**。

但 flash-lite 有一处明确的短板，同一批实测里暴露得很干净：**它对非语音声音事件不敏感——三声与人声同响度（−18.5 dB，静音段 −91 dB）的 1000Hz 纯音，8 次全部漏掉**，其中 5 次还明确答了"无背景音乐或音效"。对照主模型在拿到原生音频时数得出三声（虽然数量答过 2/3/4）。

这条短板划定了能承诺的范围：

| 能真判 | 不能顺着推 |
|---|---|
| 整体音质、音色、语调、语速、配乐风格、TTS 合成痕迹 | 单次爆音、削波、一闪而过的杂音这类**瞬态**缺陷 |

所以 `AUDIBLE_AUDIO_NOTE` 里明确写了"短促的瞬态声音你未必能注意到，没听出来时记 not_observable，不要写成「无此问题」"——不写这句，"没注意到"会被写成"没有问题"。

#### 接入形态

音频进模型有三条处境不同的路，代码里由 `AgentKClient._translate` 按阶段分派（详见 `providers/agentk_audio.py` 的模块说明）：

| 处境 | 做法 |
|---|---|
| 视频自带音轨 | 随 mp4 直接进模型，一直如此 |
| 参考音频 + 4a 对照阶段 | 音频本体进 `mediaUrls`，模型走 `audio_model` |
| 参考音频 + 其他阶段 | 完全送不进去，提示词明说"听不到" |

第三条不是保守，是主模型上有一半概率静默降级——不送反而更安全：提示词写明听不到，判据会老实记 not_observable，而不是拿一份没有听觉依据的结论当真。这一行里"有这么一段音频"仍然要说出来，不说的话模型会把音频要求当成没有要求，子判据被判成 pass 而不是 not_observable。

五处配套改动：

1. **模型按阶段可覆盖**（`config.AUDIO_STAGES` + `AgentKClient.model_for`）。音频阶段走 `audio_model`（默认 flash-lite，`AGENTK_AUDIO_MODEL` 可覆盖），其余留在主模型——把整条链换成 flash-lite 是拿视觉精度换音频精度。成本记录的兜底模型名也按阶段取，否则音频调用会归错账。
2. **4c 音频独立复听默认开**（`JUDGE_AUDIO_RECHECK=1`），但仅在视频真有音轨时发起，所以没声音的 case 一次也不多花。它此前默认关，理由是"多数 case 音频不是关注点"，而那个理由有一层没写出来的前提：音频判据本来就判不了，这次调用买不到东西。
3. **参考音频原生送**，且注入措辞跟着换。措辞若停在旧的"你只能拿到文字"，音频送到了也会被提示词自己压制掉，而表现只是音频判据依旧未核实，看不出是措辞的锅。
4. **降级探测器**：4c 的返回加 `can_hear_audio`，为假时三项音频判据一律退成 not_observable 并写明原因（`normalize_audio_recheck`），同时发一条进度事件。只认显式 `False`——字段缺失是"模型没答"，不是"没听到"，把缺失也当降级会在模型偶尔漏字段时整维清空。

5. **参考音频对照阶段（4a）**，`_compare_reference_audio`。只在 case 带参考音频时发起，把参考音频与生成视频**一起**送给 flash-lite 做听觉对照。

第 4 条平时不该触发，因为音频阶段固定走实测稳定的 flash-lite。它存在是因为触发条件不在我们手里：平台改一次行为就可能回来，而回来的时候没有任何报错。`tests/test_audio_degradation.py` 钉住这条防线，包括"提示词与归一层必须同时认这个字段"——只改一边，防线永远不触发且毫无迹象。

#### 为什么参考音频要单独一段

它进不了 4b：4b 跑在主模型上，拿不到音色与语调，而"配音是否符合参考"正是要听这些。也进不了 4c：4c 的职责是判生成视频**自身**的音频质量，明确不评价声音内容是否符合 Prompt，把参考塞进去会让那三个子判据的口径变形。

关键约束是**它只产出事实、不产出判定**。判定权归 4b——它是唯一的事实底稿作者。4a 把对照结果写成一条观察器事实，4b 读到后自己决定 `instruction_requested_text_audio` 与 `reference_input_fidelity` 怎么判。这样既不用改合并逻辑，也不会出现两个作者对同一维度各写一份。

那条事实**完全复用观察器事实的既有形状与契约**，这是它能被用上的关键：

- ID 续着 `OBS-*` 命名空间往下发（`normalize_observer_facts` 已经发到几号就从下一号开始）。4b 的提示词告诉模型观察器事实由程序分配稳定的 `OBS-*` ID，回引时也用这套 ID——塞一个别的形状的 ID 进同一个列表，模型看到的是一条来源不明的记录。
- `kind` 用 `observation` 而不是 `focus`。focus 行会触发 4b 的强制逐条表态与审计校验，那套机制是为"观察器看不清的时刻"设计的，而这里是一份已经查清的描述，供引用即可。
- `subcriteria` 填上面那两个键。这是既有的路由机制，4b 按它把事实归到对应判据下，所以**不需要往 4b 的共享系统提示词里加解释**——带参考音频的 case 是少数，不该让所有 case 都读那几句。
- 事实正文里点明"由能直接听到音频的模型独立完成，主判模型本身听不到独立音频"。不说这句，4b 没有依据决定该不该采信一份关于音色的描述。

它接在 `_stage_observe` 末尾而不是单开一个 activity——单体与阶段化两条路径都调 `_stage_observe`，一处改动两边生效，等价性测试不用动。

实测（case_032，带一段参考音频）：两边都听到了，判出"音色、内容、语调及语速高度匹配，应为同一合成语音源"——转写给不了这个判断，它只能对台词，说不出音色与播音腔。事实以 `OBS-006` 进入 4b 输入，4b 的输出确实引用了它，`instruction_requested_text_audio` 与 `reference_input_fidelity` 都给出了 pass。整条链是闭合的。

报告页有对应区块（`4a · 参考音频对照`，排在 4c 之前，因为它是喂给主判的输入而不是对主判结果的复核）。两个"没听到"标记也会单独出一行——音频判据为什么全是未核实，那是唯一的线索。

以下是这条通道其余的**真实能力损失**，都是平台能力所限而非实现取舍：

- **单次素材上限 10 件。** 合并扫描默认送「整段视频 + 16 张高清帧」共 17 件，超限。客户端会保留视频、对帧做**均匀采样**降载（比截断前 N 张更能保住时间轴覆盖），并把裁剪事实写进 prompt、`usage` 和 trace。想完全避免丢素材，设 `JUDGE_UNIFIED_SCAN_FRAMES=9`（`run_agentk.sh` 已默认这么做）。
- **没有 token 用量。** `understand_media` 的返回只有 `text` / `model` / `params`，不含 usage。报告里的成本统计会是 0，并带 `tokens_unavailable: true` 标记——是缺失，不是真的零成本。
- **没有 systemInstruction 字段。** 已并入 prompt 开头。
- **没有 JSON mode。** 靠提示词约束输出形状，加上复用原有的 `extract_json_object` 修复级联兜底。
- **没有 temperature。** 重试只能依赖模型自身的非确定性，无法像直连时那样上调温度换一条解码路径。

> 离线批量脚本（`batch/run_dataset.py` / `run_gold50.py` / `run_0807.py`）把通道**写死为 `agentk`**，不读 `DEFAULT_PROVIDER`——基线的可比性依赖固定通道，不该随部署配置漂移。
- **交错语义丢失。** 媒体和文字被彻底分开，原本「文字—图—文字—图」的顺序对应关系，靠在 prompt 里插入「【素材 N】」锚点 + 附素材清单来补偿。

### 自检

```bash
uv run python tests/test_agentk_client.py    # 假 AgentK + 假对象存储，全链路离线联调
```

worker 与 studio 侧另有几组，都不联网、不花模型钱（只有最后两组需要本机 `temporal server start-dev`）：

```bash
uv run python tests/test_worker_stages.py         # 单体与阶段化两条路径产出同一份报告
uv run python tests/test_worker_prepare_inputs.py # 参考音频与 mode 覆盖真的透到底
uv run python tests/test_studio_capability_job.py # studio 与 worker 之间那几处跨进程约定
uv run python tests/test_stage_state.py           # 阶段状态：跨实例、键不互覆盖、失败必抛
uv run python tests/test_worker_stage_workflow.py # 编排：顺序、缓存短路、进度中继、按段续跑
uv run python tests/test_worker_kill_resume.py    # SIGKILL 掉实例：只从被杀那一段重跑，且模型调用不重发
```

假服务端用的是**官方生成的 Connect server stub**（`AgentKServiceWSGIApplication`）：客户端用 SDK 的 stub 发、服务端用同一份 proto 的 stub 收，两边对上才说明传输、编码（默认二进制 protobuf 而非 JSON）、过程路径都合契约。手写假服务只能证明"我们和自己的假设一致"。

覆盖直调形状与 `input` 字段集、超限降载、内容寻址去重、"没有多余 RPC"（无建会话、无轮询）、纯文本分流到 `generate_text`、并发下同步桥不串包，以及错误分流按 Connect 错误码。

> 错误分流那一组用的是另一个裸 Connect 假服务，不是上面那个 stub。原因是 `ConnectWSGIApplication` 的**错误**响应在 wsgiref 下发不出去（服务端不写响应、客户端一直等到超时，成功路径正常）；而 Connect 的错误应答本身就是协议规定的「非 200 + `{"code","message"}` JSON」，裸 HTTP 能如实复刻，官方客户端 0.00s 就能解析。用最笨的方式是为了不把库的一个缺陷伪装成我们的错误分流通过了。

### 作为 Worker 被调用

`worker/` 把质检发布成一个 capability，别人的 agent 既能当工具（模型自己决定调）、也能当流水线（下游显式启动）用。契约集中在 `worker/abi.py`，不要在别处重复这些字符串：

| 项 | 值 | 约束来源 |
| --- | --- | --- |
| Capability ID | `contrib.aigc_eval.video_judge` | 平台要求 `contrib.<owner>.<name>`，每段 `[a-z][a-z0-9_]*`，启动时校验 |
| Task queue | `contrib.aigc-eval` | 不能用核心侧的 `agentk`，SDK 会拒 |
| Workflow type | `AigcEvalVideoJudgeV1` | 仅 worker 内部，不进 Profile |
| 声明超时 | 6 小时 | SDK 上限 24 小时。activity 预算 72 分钟 + 模型等待 90 分钟（15 次尝试 × 360 秒）+ 派发余量 5 分钟 = 167 分钟；取 6 小时是为了再盖住一整轮阶段重试。`workflow.py` 导入时就断言这笔账 |
| 重试 | 能力声明 `Tool(max_attempts=1)`；每个阶段 `maximum_attempts=2` | 平台层不重复整次质检；每段那一次重试专门接住进程/容器丢失，见下文 |

**八个 activity 而不是一个**：`prepare` / `compile` / `observe` / `scan` / `recheck` / `aggregate` / `evidence` / `report`，与 `judge/pipeline.py` 的阶段函数一一对应。理由见下一节——Temporal 的续跑只发生在 activity 边界上。阶段之间的状态经对象存储传递（`worker/state.py`），workflow 历史里只走一个 key 加一点摘要，因为 Temporal 单个 payload 上限 2MB，而 4b 的事实底稿在长素材上会顶到那个量级。

其中含模型调用的五个（`compile` / `observe` / `scan` / `recheck` / `evidence`）是**可重入**的：跑到需要模型时把请求交回 workflow，调完再带着响应重放（见「模型调用：worker 走 Temporal，不走网关」）。模型请求与响应本身也过 workflow 历史，但都是小体积——在 100 份真实 trace 上量过，最大请求 45.4 KB（4c）、最大响应 66.9 KB（4b），单次 run 合计约 310 KB。素材不在里面，它们上传对象存储换成 URL。

**进度**为什么要绕一圈：`report_progress` 只能在 workflow 里调，而阶段事件只在 activity 侧的 `on_event` 回调里出现。于是每个 activity 按秒成批 signal 回 workflow，由 workflow 转成 `report_progress`；下游在 `invocation_progress` 事件里逐阶段看到。`step_id` 带 status 后缀（`stage.done`），因为平台按 `step_id` 做幂等，同一阶段的 running / done 共用 ID 会互相覆盖。

**入参**里素材一律是 HTTP(S) URL（worker 与调用方不共享文件系统）：`videoUrl` 是待质检视频，`sourceVideoUrl` 给 V2V 的源视频，`referenceImageUrls` / `referenceAudioUrls` 是参考图与参考音频。源视频要单独走 `sourceVideoUrl` 而不是混进参考素材——给了它就按 V2V 判定，混着传会被当成普通参考视频，V2V 那三个原生子判据便拿不到基线。`mode` 可强制指定生成模式，缺省按素材自动判定（有源视频即 V2V，否则有参考图即 I2V）；纯音频参考不改变模式，其内容与风格要求归指令遵循。

**产物**同时出现在两处：`reportUrl` / `reportJsonUrl` 是既有调用方在读的字段，`artifacts[]`（`type` / `uri` / `description`）是平台约定——AgentK 只从这个名字的数组里提取产物放进 `tool_exec_succeeded.artifacts`。报告上传失败不影响结论：`artifacts` 为空、原因写进 `reportWarning`。

产物有两份：`reportUrl` 是给人看的 HTML，`reportJsonUrl` 是完整报告数据。后者存在的理由是对外输出只是**结论投影**，要自己渲染详版报告（证据帧、子判据、审计轨迹）就得拿原始报告。导出前会把报告里的帧路径改写成对象存储 URL——不做这一步，导出的 JSON 只对与 worker 共享文件系统的调用方有用，因为证据帧全是 `/.../runtime/work/...` 这样的绝对路径。帧在模型调用时多半已经传过，`BlobStore` 按内容寻址去重，所以这步基本不产生额外上传（实测一份报告 54 处帧引用去重成 16 次上传）。

**Profile**（`worker/profile.json`）里 `defaultModel` 是协议必填，哪怕只跑 Pipeline 也要给；它不承载 workflow type、task queue 和超时。

### 崩溃与重启：能续到什么程度

Temporal 的持久性**粒度是 activity 边界**：已完成的 activity 结果进 workflow 历史，实例重启后不会重跑；而一个正在执行的 activity 丢了，只能整个重来。所以"拆多细"直接决定"崩了损失多少"。质检因此拆成八个 activity，一段一个阶段。

| 中断 | 结果 |
| --- | --- |
| 调用方掉线、网络中断、客户端进程被杀 | **不影响**。质检在服务端照跑，重连后 `watch_events` 从历史重放，结果留在执行历史里 |
| worker 崩溃、容器重启、实例被驱逐 | 只损失**当前那一段**。心跳（5 分钟）超时后 Temporal 重新派发该阶段，前面跑完的阶段直接取历史结果 |

这条性质是实测的，不是推断，而且分两层各测了一遍——**"阶段不重跑"和"模型调用不重发"是两件不同的事**，前者由 Temporal 保证，后者要靠重放代理正确复用响应才成立。

**本机（`tests/test_worker_kill_resume.py`，真 SIGKILL + 本机 Temporal）。** 起两个 worker 进程，等第一个跑到 `scan` **并且已经拿到该段的模型响应**时对它 `SIGKILL`（断电式消失，不做收尾）。断言：

- `prepare` 只跑一次、`compile` / `observe` 各跑两趟（交请求 + 带响应回来），全在进程 A；
- `scan` 跑三趟——A 交请求、A 带响应回来（在这里被杀）、B 以 `attempt=2` 接手；
- `recheck` / `evidence` 各两趟、`aggregate` / `report` 各一趟，全在进程 B；
- **模型调用总数 5 次，`scan` 那次只发生过一次。**

最后两条是重点。停顿刻意放在模型调用**之后**：放在之前的话被杀时这段还没花过钱，重跑自然不重复，等于什么都没测。计数用一个桩 activity 而不是普通函数——workflow 代码在重放时会被重新执行，普通函数会跟着再调一次，计数里就混进了重放的假象。

**staging（真实平台，2026-08-31）。** 泳道 worker 跑真实质检，在第 4 次模型调用之后 `SIGKILL`，随即重启。查 Temporal 历史（`worker/count_model_calls.py`）：

| | 被杀那次 | 两次干净跑 |
|---|---|---|
| `START_CHILD_WORKFLOW_EXECUTION_INITIATED`（= 真实模型调用） | **5** | 5 / 5 |
| `ACTIVITY_TASK_SCHEDULED` | 37 | 37 / 37 |
| `WORKFLOW_TASK_TIMED_OUT` | **1** | 0 / 0 |

`WORKFLOW_TASK_TIMED_OUT` 那条是中断的证据（干净跑里没有），模型调用数没跟着涨。

> 计数只能查 Temporal 历史，**不能数 worker 日志**：重放会把日志行重打一遍。这个坑很实在——本项目早先有一次"kill 测试通过"的结论就是错的，那条 run 的历史里没有任何超时或失败事件，说明 kill 根本没落到运行中的执行上，5 对 5 比的是两次都没中断的跑。

拆开之后还有两层各自的问题要接：

- **产物**（帧图、拼接图、放大帧、证据图）在别的实例上不存在。`judge/artifact_store.py` 把它们按内容寻址进对象存储，每个阶段开工前还原自己要用的那批。
- **同一段内部**重跑仍会把模型钱再花一遍。两层各管一段：
  - 一次 run 之内，worker 路径靠 workflow 历史——模型响应是 `invoke_capability` 的结果，阶段重跑时直接从历史取，不重发；
  - 跨 run（同一条视频重复质检、开发调试循环）靠 `judge/model_checkpoint.py`，按**整个请求的哈希**（provider、模型、stage、`parts` 里的文本与 base64 内联媒体、system、温度、输出上限）缓存结果。哈希在**上传之前**算，所以命中时连素材上传都省掉。
这里原先还有一条讲音轨转写怎么按内容哈希做进程级缓存。转写已下线（见「音频」一节），但它留下一条通用教训：**重放路径上任何直接发出去的调用，失败也必须缓存**。成功缓存了、失败没缓存，等于每一遍重放都重付一次满额超时，代价随阶段内模型调用次数**相乘**放大——那正是把 observe 阶段顶穿的原因。

只有确定性错误才会一次就返回：activity 把应用层异常（代码 bug、契约校验失败、素材解不开）统一转成 `non_retryable` 的 `ApplicationError`，每段那一次重试只留给进程丢失。两个方向都有测试钉住（`tests/test_worker_retry.py`，真跑 Temporal）。

| 变量 | 作用 |
| --- | --- |
| `AIGC_EVAL_BLOB_*` | 配了就用对象存储存阶段状态、产物与检查点——容器里唯一跨重启的存储，而重启正是要解决的场景 |
| `AIGC_EVAL_CHECKPOINT_DIR` | 本地目录后端的位置，默认 `runtime/checkpoints`（开发机 / studio 用） |
| `JUDGE_CHECKPOINT=0` | 整个关掉 |

没配对象存储时，阶段状态退到 `runtime/worker_state`，产物留在本地——单机联调可用，但那时所有阶段必须落在同一台机器上。

`refresh=1`（忽略缓存重跑）会跳过检查点的**读**、照常写：用户要的是重新判一次，不是把上次的答案端出来。报告里的 `checkpoint` 字段记命中数与后端（跨阶段累加），命中数为 0 的重跑说明键没对上（素材、提示词或模型变了）。

**两条路径必须给出同一份报告**：批处理脚本走单体 `analyze_case`，worker 走八个阶段。（studio 已改走 capability，但 `analyze_case` 不能删——它既是批量入口，更是证明分阶段路径没改变结论的那把尺子。）`tests/test_worker_stages.py` 在同一个进程里把两条都跑一遍（模型响应来自 `runtime/traces` 的真实录制，不联网），逐字段对报告、逐条对前端看到的阶段事件序列、并核对 trace 步数与成本合计。段间的状态每次都过一遍 `json.dumps`，因为真实 activity 边界就是这么传的——哪一段往状态里塞了 `Path`，测试当场就炸。

这条测试同时也是重放的护栏：阶段化那一侧走的是真的驱动循环（在进程内模拟 workflow 的 build / invoke / consume），所以"重放没有让某个阶段多记一条 usage、多发一条事件、或漏写一份产物"是被逐字段比出来的，不是读代码确认的。回放替身也随之下移了一层——从"假 client"变成"假 invoke"（`recorded_responder`），更贴近真实边界。

**联调形态**：worker 与调用方不在同一台机器上。`connect_temporal()` 经 KESS 发现
Temporal，只能跑在有 kess-agent 的环境——所以 worker 走容器云的一个非基线泳道实例
（部署单元里显式给 `KWS_LANE_ID=dev/<用户名>`）。调用侧留在开发机：客户端默认用
HTTP transport，按 `KWS_SERVICE_STAGE` 推导网关，不需要 kess-agent，且支持完整
RPC 面（含流式 `watch_events`）。

```bash
# worker 侧（泳道实例内，或任何有 kess-agent 的机器）
KWS_SERVICE_NAME=klingai-agentk-aigc-eval-api ./deploy/run_worker.sh

# 调用侧（开发机）。两种模式对应两条完全不同的路径，上线前都要验：
# pipeline —— 调用方点名能力并给全部参数，验路由、schema、进度、产物
uv run --no-sync python worker/verify_agentk.py --lane dev/<用户名> \
    --video-url https://... --prompt "一只猫走过草地"
# tool —— 只发一句不含提示词的话，由模型自己决定调用与填参
uv run --no-sync python worker/verify_agentk.py --mode tool --lane dev/<用户名> \
    --video-url https://...
```

`--lane` 不是可选项：泳道由**调用方**声明（SDK 以 `agentk-lane-id` 头带出），漏了就
走基线队列。那时的表现是执行一直排队、没有任何进度事件——看着像 worker 没起来，其实
是打到了另一条队列上。两侧取同一个值。

调用侧这条链路已在 staging 验到 `start_execution` 为止：Session 建得起来、`worker/profile.json` 过了平台校验、pipeline 声明被接受、泳道头也带上了。再往后就必须有 worker——没有实例认领时 `start_execution` 报 `service unavailable`，脚本会追一句提示，因为联调期这个错几乎总是「worker 没起」或「两侧泳道对不上」，而后者最难认：worker 日志一切正常，它只是在另一条队列上等。

`--mode tool` 盯的是一个 pipeline 模式永远暴露不出来的风险：`prompt`（生成该视频所用的提示词）只有用户知道，**模型拿不到时可能自己编一个填进去**。那样报告照常产出、看起来完整，但"指令遵循"判的是模型编的要求——一个不报错也不显示异常的错误结论。脚本会把模型实际填的参数打出来并给出 PASS/FAIL：编造判 FAIL，先向用户索取判 PASS。profile 的 `system` 里写了"缺提示词先索取"，但那是期望不是保证。

### 基线部署（让别人能调）

那个[能力目录](https://agentk.staging.kuaishou.com/docs/capabilities)是**活的**：contrib 能力靠 worker 持续刷新的注册租约存在，worker 一停条目就消失。所以"发布"不是一次性动作，而是把 worker 常驻起来——`worker.publish_workflow(...)` 已经是发布本身，不需要另外去哪里登记。

走 `Dockerfile` + `deploy/container/start.sh`：天琴注入 `KWS_SERVICE_STAGE` 且不给 `KWS_LANE_ID`，worker 以基线身份轮询基础队列。**别用 `deploy/run_worker.sh`**——它会显式给 `KWS_LANE_ID`，那是联调用的隔离。

KDev 的「构建 Docker 镜像」（Build_Image_V2）插件里，只有一个字段需要偏离默认值：

| 字段 | 填什么 | 为什么 |
| --- | --- | --- |
| Dockerfile 路径 | `Dockerfile`（默认值） | 本仓库是独立 git 仓库，流水线工作空间根就是项目根 |
| 构建上下文 | `.`（默认值） | `COPY src ./src` 一族都相对项目根，指到别处 COPY 会失败 |
| 构建镜像格式 | `linux/amd64` | 基础镜像与静态 ffmpeg 产物都是 amd64，留空可能构出跑不起来的架构 |
| DOCKER_BUILDKIT | `false`（默认值） | 没用 BuildKit 专有语法，多阶段构建普通 docker build 就支持 |

镜像按容器云「自定义服务」约定组织，与 `agentk-capability-demo`（仓库里唯一的 Python 先例）同构：基础镜像用容器云的 Python + uv 镜像（`infra-*` 一族只在内部 PyPI 上，公网源解析不到），`kcsize` 作 1 号进程、通过 `START_COMMAND` / `STOP_COMMAND` 托管 `start.sh`，HTTP 侧监听平台分配的 `AUTO_PORT0`。ffmpeg 用与 `agentk/Dockerfile` 同一份静态产物（校验和一致），基础镜像里没有它，而质检的前置检查、抽帧、证据帧、音轨提取全靠它。

两个进程**同生共死**而不是交给 supervisor 分别重拉，是刻意的：worker 一停，注册租约到期后能力就从目录里消失，调用方看到的是「能力不存在」，而此时 HTTP 侧的健康检查还一路绿灯——这种半死状态比整体挂掉难查得多。

部署单元按 http 服务配，端口不用填——`AUTO_PORT0` 由平台分配、`start.sh` 转成 `VIDEO_JUDGE_PORT`。

健康检查填 `/healthz`，期望值 `{"status":"ok"}`（与 `agentk-capability-demo`、`maas-worker` 同一约定）。**期望值里不要加空格**：readiness 探针拿返回体做字符串精确比对，`{"status": "ok"}` 与 `{"status":"ok"}` 肉眼几乎一样但比不过，所以 `/healthz` 特意输出紧凑 JSON（`_send_json(..., compact=True)`）来对齐平台那份配置。**别指到 `/api/health`**：那个端点是给页面看的诊断信息，恒返回 200 且 body 里没有 `status` 字段，期望值匹配不上；而且它报的是"配置齐不齐"而非"进程活没活"，拿配置问题去触发容器重启是错的靶子。`/healthz` 只回一个常量就够——`start.sh` 让 worker 与 HTTP 同生共死，HTTP 还能应答就意味着 worker 也在。

另外必须显式给这两个变量，它们不像 `KWS_*` 那样由平台注入（endpoint 是 `https://bs3-hb1.staging.kuaishou.com`，桶名向平台侧索取；staging 与 prod 用同一份）：

| 变量 | 缺了会怎样 |
| --- | --- |
| `AIGC_EVAL_BLOB_ENDPOINT` | 素材传不上去。`understand_media` 只收 HTTP URL，每次质检都在上传阶段失败 |
| `AIGC_EVAL_BLOB_BUCKET` | 同上 |

漏这两个的后果本来最难查：capability 能 announce、能接单，然后每一次调用都失败——从调用方看是"能力在目录里、调过去就报错"。所以基线实例**缺它们就拒绝启动**（退出码 2），容器反复重启一眼可见；联调实例只告警。

两个环境相关的坑，都已在代码里挡掉，但值得知道：`TEMPORAL_ADDRESS` 在基线实例会被忽略（镜像里残留这个变量会让 worker 静默连到空 namespace，表现为"启动正常、日志干净、永远收不到任务"）；`AGENTK_BASE_URL` 不要在部署里设——它会把传输强制成 HTTP 固定网关，而生产上那条不通，表现为所有模型调用 `unauthorized`。

### 部署到 staging 还是 prod

部署配置几乎不用动：`KWS_*` 由平台注入，Temporal 由 KESS 按运行环境发现，对象存储两个环境用同一份 endpoint 与桶。但有三件事会变，其中第一件我们是踩进去才发现的。

**传输通道不同，代码里按 stage 分了岔。** 生产的 HTTP 固定网关不对我们开放，只有 KESS gRPC 通（见上文「AgentK 通道」）。这一条最坑的地方在于它**伪装成鉴权问题**：报错就是 `unauthorized`，和「服务名没在这个环境登记」一字不差。我们据此找平台侧加白，白等了一轮，直到把两条通道并排跑才看出来——身份其实一直是好的。所以在 prod 上遇到 `unauthorized`，**先跑 `whoami_probe.py` 分清是通道还是身份**，再去找人。

**我们写死的模型有两个是 preview**（`gemini-3.1-pro-preview`、`gemini-3.1-flash-lite`），加上 `deepseek-v4-pro`，在新环境未必都开放。

**能力会注册进该环境的目录。** 部署到 prod，条目就出现在生产目录里，而 `profile.json` 把它声明成了给模型自己调的 tool——别人的 agent 能发现并调用。注意能力注册走 Temporal、模型调用走网关，是两条独立的路：网关这侧不通时，能力照样在目录里挂着，调过去必错而健康检查全绿。跑通冒烟前先别对外宣告。另外本机联调的泳道 worker 连的是 staging Temporal，所以本机那套验证不了 prod 部署。

容器里有两个诊断脚本，先身份后链路：

```bash
python3 /home/web_server/aigc-eval/bin/whoami_probe.py   # 免费，可反复跑：身份 + 两条通道并排
python3 /home/web_server/aigc-eval/bin/smoke_check.py    # 付费一次：身份、对象存储、三个模型
```

`smoke_check.py` 的做法是上传一张纯色 PNG，让模型回答它是什么颜色。答对说明身份认了、模型可用、且 AgentK 那侧成功取到了我们的桶——任一环断掉都会在这一步失败，脚本按错误文本区分是哪一环。问「什么颜色」而不是「这是不是一张图」，是因为后者在素材取不到时模型也能糊过去。

两个脚本都会先从服务进程借身份：**`KWS_SERVICE_CREDENTIAL` 只注入到 `START_COMMAND` 拉起的进程**，`kcsize`（PID 1）和 WebConsole 另起的 shell 都没有。在 shell 里直接手工验会得到 `unauthorized`——又一个和「没授权」长得一样的假象。

健康检查绿了不能替代这两步：它只说明 HTTP 端口活着，上面几件事一件都没碰。

> 上线前待 AgentK 开发者确认的接入选择、实测到的平台行为、以及尚未验证的部分，整理在 [`docs/AGENTK接入自查.md`](docs/AGENTK接入自查.md)。

## 管线

```
① 视频预检 → ② 运动感知抽帧/联系表 → ③ Prompt 拆解为约束
  → ④ VLM 观察（只记事实） → ④b P0 维度合并扫描 → ⑤ VLM 验证（逐条裁决）
  → ⑥ 确定性加权 + 硬门控 + 一票否决 → ⑦ 证据帧 + HTML 报告
```

**① 视频预检**（程序）：ffprobe 元信息 + blackdetect/freezedetect + 帧数校验。这是**前置 sanity 门控，不再是加权维度**——文件异常直接拦，不参与算分。

**② 运动感知抽帧**：用 `tblend=difference + signalstats` 的逐帧运动量（纯 ffmpeg，无 numpy/opencv）识别高运动窗口，在均匀基础网格之上对高运动区间加密采样，并把「高运动加密帧」标注传给观察器——针对旋转连续性、穿模等高频运动问题。探测失败时回退均匀抽帧。

**④b 合并扫描**（默认路径）：一次模型调用判完 一致性 / 指令遵循 / 崩坏 / 画面异常 / 文字 / 故事逻辑 六项，输入是整段视频 + 至多 16 张带镜头号的高清帧（长边 ≤1280）。这条路把 4b 从约 37 次调用压到 1~2 次。置 `JUDGE_UNIFIED_SCAN=0` 可回退旧的分维度专项检测器路径（成本约 5 倍）。

另有条件触发的**高清复核**：仅对初判有风险且可定位的疑点抽原生帧再看一次（≤4 帧）。

**⑥ 之后还有一步问题定位**：把已定论的问题框到画面上（bbox），供报告页的视频组件跳转高亮。它跑在打分**之后**，只补坐标、**不参与判定**。

## 评测维度（固定三级体系，16 个叶子）

每个视频都按一级 / 二级 / 三级维度逐条核对（`rubric.py`），模型对每个叶子给 `pass` / `violated` / `not_observable` / `not_applicable` + severity(0-3)，报告里按三级分组展示。

| 一级 | 二级 | 三级叶子 |
|---|---|---|
| 一致性 | 参考图一致性 | 主体 ID / 关键属性 / 色彩画风与参考图一致（支持多张 `Image N` 逐张核对） |
| 一致性 | 多镜头间一致连贯性 | 相邻镜头的主体、道具、场景与情节状态连续 |
| 一致性 | 动态时序一致性 | 同一镜头内主体 / 环境 / 数量随时间无凭空改变 |
| 指令遵循 | 基础内容 | 主体、动作、对象、风格与环境按 prompt 响应 |
| 指令遵循 | 多镜头 | 镜头数量、顺序、切镜时点与镜头内信息按 prompt 响应 |
| 指令遵循 | 运镜文本响应 | 运动类型 / 方向 / 速度 / 景别按 prompt 响应 |
| 指令遵循 | 音频指令遵循 | 台词、音效、音乐与时机按 prompt 响应 |
| 画面质量 | 画面基础内容 | 曝光光影或色彩异常 · 油腻感（AI 塑料感） · 文字不可读或与要求不一致 |
| 动态质量 | 动态基础质量 | 不符合物理规律（守恒 / 接触即反应 / 无凭空生灭 / 重力惯性流体） · 明显崩坏 |
| 音频评测 | 音画同步 | 口型与台词、音效与画面事件是否对齐 |
| 提示词一致性 | 时空关系与动作目标 | 相对位置 / 朝向 / 动作-目标 / 时序 与 prompt 一致（按世界坐标而非屏幕左右判定） |
| 提示词一致性 | 环境与场景响应 | 场景地点、天气光照、时间季节、背景关键元素按 prompt 呈现 |
| 故事逻辑 | 故事逻辑连贯性 | 模型自行补充的情节是否合理连贯（prompt 明确的情节即使不合理也不算错） |

有程序化适用性兜底：无图参考 → 参考图一致性记 `not_applicable`；镜头数 < 2 → 多镜头连贯性不适用；无音轨 → 音画同步不适用。避免模型对不相关叶子误报后触发扣分或门控。

`not_observable` 是**一等结果**：遮挡 / 模糊 / 分辨率不足时不得强判错误。

## 打分与拦截（全部确定性，`scoring.py`）

**叶子如何影响维度分**：每条 `violated` 按 severity 扣分（轻微 -8 / 明显 -22 / 严重 -40；油腻感、曝光色彩这类观感项打 6 折），扣分累加到它所属维度。

- **扣分只会拉低维度分，不会拉高**：最终维度分 = `min(模型给的分, 100 - 该维度扣分)`。模型偏乐观时以扣分后为准。
- **画面异常与文字异常两维纯由 rubric 驱动**，完全不采信模型自评，避免内容指令类问题串味。
- 模型没给某维分时按「缺失」处理，不当 0——否则一个遗漏就把维度打死。

**加权维度 5 个，两套权重**（模式判定：有图参考 → I2V，否则 T2V）：

| 模式 | 主体一致性 | 指令遵循 | 动态崩坏 | 画面异常 | 文字异常 |
|---|---:|---:|---:|---:|---:|
| T2V | 20% | 30% | 20% | 20% | 10% |
| I2V | 30% | 25% | 20% | 15% | 10% |

「故事逻辑连贯性」叶子**有意不进加权总分**（只服务标注口径），以保住 `score_total` 与历史缓存基线的可比性。

**两级拦截**：

- **硬门控** → `status=blocked`（不封顶总分）：崩坏≥明显 / 任何 severity=严重 / 硬约束≥明显 / 文件异常≥明显 / 模型自判 blocked。模型自报的 blocked 永不下调。
- **一票否决** → 总分直接封顶 **20** 且 blocked：仅两条——崩坏等级 ≥ 明显、曝光/色彩异常 = 严重。

硬失败（明显崩坏、关键穿模、关键 ID 丢失、关键参考漏响应、关键 prompt 事件失败）**不能**被高画质总分抵消。报告的「各维度分数与核心问题」表格会显示每个维度的分数、权重、以及扣分来源（维度叶子 / prompt 要求 / 文件检查）。

## 标注口径（交付用）

内部体系（16 叶子、5 加权维度、severity 0-3）与人工标注规范不是一套东西。`annotation.py` 做一层**只读的单向映射**，恒定输出 8 个维度 + 三档得分：

| 标注维度 | 得分档 |
|---|---|
| 指令遵循 · 参考一致性 · 视觉一致性 · 音频综合质量 · 画面质量 · 动态质量 · 文字质量 · 故事逻辑连贯性 | `0.0` 恶劣错误 / `0.5` 瑕疵 / `1.0` 无问题 |

三条性质：**只读不回流**（跑在打分之后，不改 `score_total`）、**纯函数**（可离线重跑在任意历史报告上，零模型成本）、**不把「没判过」说成「没问题」**（未评测与不适用分开记）。

## 大模型如何看视频

图片 / 视频都以 base64 `inline_data` 传给模型：

- **看视频**：默认把**整段视频**（`video/mp4`）直接交给模型（原生视频理解）。超过 `GEMINI_VIDEO_MAX_MB`（默认 **512**）时自动改为「按运动量加密抽取的关键帧 + 全片缩略拼图」。`GEMINI_SEND_WHOLE_VIDEO=0` 可强制走抽帧。
- **看提示词**：全文交给模型，拆成一条条可核对要求。
- **参考图 / 参考音频**：一并传入，按 `Image N` / `Audio N` 标签逐个核对。

模型 JSON 输出经多级修复级联解析（去尾逗号 / 截断括号平衡 / 未闭合字符串修复），并对解析失败自动重试，避免偶发格式问题被误判为调用失败。429/5xx 走指数退避重试。

默认走**真实大模型**；取消勾选可用离线占位（仅开发用）。Live 失败时可回退 Mock，并在 Audit Trace 中标明原因。

## 运行方式（轮询）

分析在后台任务里跑，浏览器轮询进度（`POST /api/upload_analyze` 拿到 `job_id` → 反复 `GET /api/job?cursor=N` 增量拉阶段事件），避免长连接被中断。运行过程以「每一步的调用 / 输入 / 输出」对话式呈现，限流退避重试作为 `notice` 实时提示。

**结果缓存复用**：素材按内容寻址算出 `slug`，每个 `(slug, engine, mode, CACHE_VERSION)` 的**完整报告持久化**在 `runtime/results/`。同一份素材 + 同一段 prompt 再提交一次**直接复用历史、不调模型**（`from_cache=true`）；勾选「忽略缓存重跑」（`refresh=1`）强制重算。Prompt 参与 slug 哈希，所以「有指令」和「无指令」是两条独立缓存。

**每次跑完自动产出 HTML 报告**：写入 `runtime/reports/<run_id>.html`，自包含（图片 base64 内嵌）。板块含判断流程、Agent 所见证据（关键帧 / 联系表）、约束账本、观察事实、约束裁决、硬失败、标注口径结论、逐步模型 I/O。

## 页面

只有一个页面：`/`（等价于 `/studio.html`）。左侧栏是历史质检记录，中栏拖拽上传或从离线数据集选材，右栏是阶段进度 + 质检报告 + 带红框标注的问题定位播放器；选中同一 case 下的多条视频时右栏切换为 listwise 横向排名。

## 历史质检记录

侧边栏的历史列表是对 `runtime/results/` 的**只读投影**，没有第二份真相 —— 每次跑完的完整报告本来就落在那里，文件名即缓存键。因此重启服务、换端口、甚至直接拷走 `runtime/` 都不会丢记录。报告单份 250KB+，列表按 (文件名, mtime, size) 记忆化摘要，只有文件真变了才重新解析。

- **标题**：自定义名字 → Prompt 首行 → 视频原文件名 → slug。Prompt 非必填，纯素材质检的记录只剩文件名能认人，所以回退链不能少这一层。行内改名落在 `runtime/history_titles.json`。
- **回看**：点一条记录会同时恢复左栏输入（Prompt、视频、参考素材）与右栏报告。素材仍在服务器上（`runtime/uploads/<slug>/`），恢复只是把界面指回那批文件的 URL，不重新下载字节。
- **改一改重跑**：恢复出来的素材标为 `origin=history`，提交时只回传 `history_slug` 与标签，不重传内容。改了 Prompt 就是改了内容寻址的输入 → 算出新 slug → 落成一条新记录，原记录不受影响。
- **删除**：连带清掉该次的报告、轨迹、中间产物与素材。两道保护：同 slug 还有别的记录（换通道跑过）时保留素材；别的 slug 的 manifest 借用了本目录下的文件（历史重跑是原地引用）时也保留 —— 否则删旧记录会让新记录的视频回放静默变成 404。

`runtime/` 至今没有自动清理策略，删除是目前唯一的回收手段。

## API

对外只保留质检这条链路需要的接口。

```
GET  /api/health                                                   # provider 与 blob 配置探活
GET  /api/job?job_id=...&cursor=N                                  # 增量拉阶段事件（前端轮询）
GET  /api/report?run_id=...                                        # 自包含 HTML 报告
GET  /api/trace?run_id=...                                         # 调用轨迹
GET  /api/upload_media?slug=...&asset=...                          # 取回已上传素材（支持 Range）
GET  /api/history?limit=N                                          # 历史质检记录清单
GET  /api/history_item?id=...                                      # 单条：完整报告 + 恢复输入所需信息
GET  /api/dataset_cases | dataset_case | dataset_media | dataset_thumb
GET  /api/case-media?case_id=...&asset=...                         # 仅供 batch/* 产出的报告取磁盘素材
GET  /api/listwise_report?id=...                                   # 横向对比报告

POST /api/upload_analyze                                           # multipart 上传素材并起质检作业
POST /api/listwise_run                                             # 起多候选横向对比作业
POST /api/history_title                                            # {id, title}，空标题即回到自动推导
POST /api/history_delete                                           # {id, with_assets}
```

`POST /api/upload_analyze` 的素材有三种来源，可混用：上传文件（`video` / `ref` + `label_<i>`）、离线数据集（`dataset_case_id` + `dataset_video` / `dataset_ref_<i>`）、历史记录（`history_slug` + `history_ref_<i>` 选条 / `history_label_<i>` 覆盖标签）。后两种素材已在服务器磁盘上，只回传标识符而非字节。

媒体分发支持 Range / 206，报告页视频可正常拖动。

## 输出契约

每条 prompt 约束的裁决：

```json
{
  "rule_id": "ROTATION-01",
  "verdict": "pass | violated | not_observable | not_applicable",
  "severity": 2,
  "quality_score": 1,
  "time": "00:00.4-00:03.2",
  "evidence": "...",
  "confidence": "medium",
  "dimension": "consistency | instruction | collapse | visual | text"
}
```

报告顶层还包含：`scores`（五维向量）、`score_total`、`dimension_summary`、`rubric`（16 叶）、`hard_failures`、`collapse_level`、`final`（内含 `status` / `summary` / `veto_reasons`）、`annotation`（8 维三档）、`evidence`、`cost`、`trace`。

## 模块

| 模块 | 作用 |
|---|---|
| `server/app.py` | HTTP 服务、异步作业 + 轮询、静态与媒体分发、listwise 端点 |
| `judge/pipeline.py` | 主编排 `analyze_case`，观察器 / 合并扫描器 / 验证器 / 定位器，案例发现、成本核算、缓存 |
| `judge/agentic.py` | 另一套 agentic 编排：给模型工具让它自主取证 |
| `judge/rubric.py` / `judge/rubric_prompts.py` | 16 叶维度定义与归一 / 各阶段 system prompt |
| `judge/scoring.py` | 权重、扣分、硬门控、一票否决 |
| `judge/integrity.py` | ffprobe/ffmpeg 预检、运动 profile、抽帧、区域裁剪 |
| `judge/evidence.py` | 证据帧生成与红框标注 |
| `judge/listwise.py` | 多候选排名与分歧标注 |
| `providers/agentk.py` | 唯一的模型通道：平台能力调用、素材翻译、重试 |
| `providers/media_io.py` / `json_parse.py` | 与通道无关的通用件：MIME/内联编码/模态盘点、模型输出的 JSON 容错解析 |
| `providers/blob_store.py` | 本地素材 → AgentK 可读 URL，内容寻址去重 |
| `config.py` | 凭证与 provider 配置 |
| `paths.py` | 全项目唯一的路径锚点 |
| `history.py` | 历史质检记录：对 `runtime/results/` 的只读投影、标题推导、重命名、连带删除 |
| `trace_store.py` | 调用轨迹全量落盘 |
| `report/html.py` / `report/listwise_html.py` | 自包含 HTML 报告 / 横向对比报告 |
| `report/annotation.py` | 标注口径适配层（8 维 + 三档） |
| `dataset/upload_case.py` | multipart 解析、素材落盘、内容寻址 slug |
| `dataset/cache.py` | 离线数据集只读浏览 + 缩略图 |

**脚本**（都以模块方式跑，`-m` 才能正确解析包内 import）：

```bash
export PYTHONPATH=src
python3 -m aigc_eval.dataset.build_cache   # xlsx → mm-datasets/<name>/cache 素材落盘
python3 -m aigc_eval.batch.run_dataset     # 按磁盘 cache 并发批跑，支持断点续跑
python3 -m aigc_eval.batch.run_0807        # 0807 数据集下载 + 批跑
python3 -m aigc_eval.batch.eval_f1         # 五个自测集的 P/R/F1（需 pandas）
python3 -m aigc_eval.batch.smoke_scan      # 单 case 冒烟，断言 5 个扫描字段都在
```

## 成本

Gemini 计费：输入 ¥11.97/百万 token、输出 ¥71.79/百万（思考 token 计入输出价）。走合并扫描时单 case 约 8–9 次调用，报告页显示每 case 预估成本。视频虽大但只占约 300 token（模型高度压缩表征），传整段视频不比传关键帧贵。

⚠️ 批量跑前确认 `JUDGE_UNIFIED_SCAN=1`：回退到分维度路径会变成约 37 次调用、约 5 倍成本。建议同时设 `JUDGE_UNIFIED_FALLBACK=0`，宁可报错重试也不要静默烧钱。

## 产物

```
runtime/results/   完整报告缓存（按 case/source/engine/mode/CACHE_VERSION 命名）
runtime/traces/    每次运行的逐步调用轨迹
runtime/reports/   自包含 HTML 报告
runtime/work/      抽帧、裁图等中间产物（一条视频一个目录，按 run_id 再分层）
```

整个 `runtime/` 已 gitignore，且**无清理策略**——长期跑会积累大量 HTML / JSON，需手动清。

## 下一步建议

1. 用带时间戳的金标继续校准崩坏召回与穿模 F1；
2. OCR 接入文字证据层，替代纯 VLM 转写；
3. 把 `analyze_case` 里的打分覆写闭包抽到 `scoring.py`，使其可单测（当前仓库无任何测试）；
4. 补一层 report JSON 的字段契约校验，避免改字段名时前后端五处静默漂移。
