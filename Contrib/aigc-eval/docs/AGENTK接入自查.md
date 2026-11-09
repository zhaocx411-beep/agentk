# AgentK 接入自查（待 AgentK 开发者确认）

本文档给 AgentK 开发者看，用来确认 `Contrib/aigc-eval` 的接入方式是否正确。
分三部分：**我们怎么接的**（请判断对错）、**实测到的平台行为**（请确认是否预期）、
**明确的待确认问题**（希望得到答复的清单）。

依赖版本：`ks-agentk-worker==0.3.2`、`ks-agentk-client`。环境：STAGING。
服务身份：`klingai-agentk-aigc-eval-api`。

---

## 0. 这个服务是什么

给一条 AIGC 生成的视频出一份可审计的质检报告：按固定的八维度体系（指令遵循、参考
一致性、视觉一致性、音频质量、画面质量、动态质量、文字质量、故事逻辑连贯性）逐维度
给三档结论（无问题 / 瑕疵 / 恶劣错误），并给出时间点与证据帧。一次质检跑 7 个阶段、
分钟级、要花掉多次模型调用。

它同时以**两种身份**与 AgentK 打交道，这两侧是独立的，下面分开说：


| 身份  | 含义                                                                         | 相关代码                                 |
| --- | -------------------------------------------------------------------------- | ------------------------------------ |
| 调用方 | 把 AgentK 当模型后端，调 `understand_media` / `generate_text` | `src/aigc_eval/providers/agentk*.py` |
| 提供方 | 把质检本身发布成一个 capability，供别人的 agent 与下游调用                                     | `worker/`                            |


---



## 1. 提供方（worker）侧：我们的契约选择

契约集中在 `worker/abi.py`，不在别处重复这些字符串。


| 项             | 取值                                                             | 我们的理由                                  |
| ------------- | -------------------------------------------------------------- | -------------------------------------- |
| Capability ID | `contrib.aigc_eval.video_judge`                                | 按 `contrib.<owner>.<name>`             |
| Task queue    | `contrib.aigc-eval`                                            | 不能叫 `agentk`——那是核心队列，SDK 直接拒绝          |
| Workflow type | `AigcEvalVideoJudgeV1`                                         | 仅 worker 内部，不进 Profile                 |
| 声明超时          | 2 小时                                                           | 容纳长素材与模型侧排队                            |
| 内部结构          | 1 个 workflow + **8 个 activity**（质检的八个阶段各一个）                    | 让崩溃只损失当前那一段，见 1.3                      |
| 重试            | 能力声明 `Tool(max_attempts=1)`；每个阶段 activity `maximum_attempts=2` | 平台层不重复整次质检；每段那一次留给进程/容器丢失              |
| 发布方式          | `worker.publish_workflow(Capability(...))`                     | 输入输出用 Pydantic 的 `model_json_schema()` |


**内部超时预算落在对外声明之内**：八个阶段各有自己的 `schedule_to_close`
（20 / 8 / 15 / 25 / 20 / 3 / 12 / 8 分钟），合计 111 分钟，加 5 分钟派发余量
= 116 分钟，落在声明的 120 分钟之内；`workflow.py` 导入时就断言这一点，改预算改超了
起不来。取 `schedule_to_close` 而不是 `start_to_close` 作为每段的预算，是因为它同时
覆盖该阶段的重试，所以"各段预算之和"就是最坏情况总耗时。

理由是声明值是调用方唯一能看到的数，内部预算超过它就等于声明了一个做不到的承诺。

→ **请确认**：这个理解对吗？还是说平台侧对 activity 预算与 capability 声明的关系
另有约定（例如声明值只作展示、实际以 workflow 执行超时为准）？

### 进度上报

`report_progress` 只能在 workflow 里调用，而阶段事件只在 activity 侧的回调里出现。
所以做法是：**每个 activity 按秒成批 signal 回 workflow，由 workflow 转成**
`report_progress`。signal 进入 workflow 历史，重放时顺序一致，所以这个中继是
确定性的。拆成八个 activity 之后这个形状没变，只是 signal 的来源从一个 activity 变成
八个；调用方看到的进度流逐条一致（有测试对比）。

`step_id` 带 status 后缀（`stage.done`），因为平台按 `step_id` 幂等，同一阶段的
running / done 若共用 ID 会互相覆盖。

### 1.3 长耗时能力的崩溃恢复（已改成 Temporal 原生的多 activity）

Temporal 的持久性粒度是 activity 边界，所以"拆多细"直接决定"崩了损失多少"。质检
因此拆成八个 activity，与判定流程的八个阶段一一对应：

`prepare`（取素材、文件检查、V2V 对齐测量、抽帧）→ `compile`（拆解提示词）→
`observe`（看视频记录事实）→ `scan`（原生八维主判）→ `recheck`（疑点复核 + 音频复听）→
`aggregate`（确定性聚合）→ `evidence`（问题定位与证据卡）→ `report`（组装报告）。

拆开之后有三件事要处理，做法如下：


| 问题              | 做法                                                                                                                                   |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| 阶段之间的状态怎么传      | 存对象存储，workflow 历史里只走一个 key 加一点摘要。不随 payload 走是因为 Temporal 单个 payload 上限 2MB，而主判阶段的事实底稿在长素材上会顶到那个量级——超限就是整次失败，且失败点随素材长度漂移             |
| 帧图/证据图在别的实例上不存在 | 按内容寻址进对象存储，每个阶段开工前还原自己要用的那批                                                                                                          |
| 同一段内部重跑仍会重花模型钱  | 两层：一次 run 之内靠 workflow 历史（模型调用改走 `invoke_capability`，响应就是子 workflow 的结果，阶段重跑时从历史取）；跨 run 靠模型调用级检查点，按整个请求的哈希缓存结果。哈希在上传之前算，命中时连素材上传都省掉 |


状态的 key 是 `<run>/<stage>.json`：每个阶段**写自己的键、读上一阶段的键**，从不覆盖
别人的，所以重试天然幂等——重跑读到的输入与第一次完全一致。

含模型调用的五个阶段（`compile` / `observe` / `scan` / `recheck` / `evidence`）另外是
**可重入**的：模型调用必须发生在 workflow 里（`invoke_capability` 底层是
`execute_child_workflow`），而组装素材与解析响应必须发生在 activity 里，两者之间又没有
workflow → 运行中 activity 的数据通道。所以这五个阶段的做法是"跑到需要模型时把请求交回
workflow，调完带着响应重放一遍"。workflow 侧因此只有一个通用驱动循环，不需要为
`recheck` 那三次有条件依赖的调用写状态机；代价是每次调用之前的确定性工作重算一遍，这也
是 `prepare` 不参与重放的原因（它是唯一可能跑到二十分钟的阶段）。

应用层异常（代码 bug、契约校验失败、素材解不开）统一转成 `non_retryable` 的
`ApplicationError`，所以每段那一次重试只会用在进程丢失上。

**已实测**（本机真 Temporal，阶段正文用假实现以便快速跑）：起两个 worker 进程，等第一个
跑到 `scan` 时对它 `SIGKILL`，前三段不重跑、`scan` 在第二个进程里以 `attempt=2` 重跑、
其后四段在第二个进程里跑完。另有一组测试在同一进程里把"单体"与"阶段化"两条路径都跑一遍
（模型响应取自真实录制），逐字段对报告、逐条对进度事件序列，确认拆分没有改变判定。

→ **想请教**：

1. 这个形态（1 个 workflow + 8 个 activity，阶段状态走对象存储、只在历史里传 key）
  与平台预期一致吗？平台对 contrib capability 的 activity 数量、workflow 历史大小
   有没有约束或建议上限？
2. 阶段状态我们自己存对象存储。平台侧有没有现成的机制（例如某种 run 级临时存储）
  更适合承担这件事？
3. 能力声明里的 `Tool(max_attempts=1)` 与 activity 自己的重试策略是两回事吗？我们的
  理解是前者管"平台要不要重发整次 capability 调用"、后者管 worker 内部，因此把前者
   保持为 1（避免整次质检被重复计费）、由后者接住基础设施丢失。
4. 一次 capability 调用中途换实例接手（前几段在实例 A、后几段在实例 B）时，
  `report_progress` 的中继与调用方的 `watch_events` 都能正常延续吗？我们这侧看不出
   问题，但这取决于平台如何把进度关联到 invocation。

→ **请确认**：这个 signal 中继是推荐做法，还是有更直接的方式让 activity 侧的进度
到达调用方？

### 产物

同时出现在两处：`reportUrl`（既有 studio 页面在读的字段）与 `artifacts[]`
（`type` / `uri` / `description`）。我们的理解是 AgentK 只从 `artifacts` 这个名字的
数组里提取产物放进 `tool_exec_succeeded.artifacts`。报告上传失败时 `artifacts` 为空、
原因写进 `reportWarning`，不影响质检结论本身。

→ **请确认**：字段名与形状是否正确？`Artifact` 的 `type` 有没有受约束的取值集合
（我们填的是 `html`）？

### Profile

`worker/profile.json`：`defaultModel` 必填（我们填 `deepseek-v4-pro`），
同一个 capability 既声明为 tool 也声明为 pipeline，`maxToolRounds: 4`。

→ **请确认**：同一 capability 同时挂 tool 与 pipeline 是否合规？

---



## 2. 调用方侧：我们的用法

一次模型调用翻译成一次 `InvokeCapability` 直调（不建 Session、不起 Execution、
不轮询），按素材类型分流：


| 调用类型 | 能力                        | 模型                       |
| ---- | ------------------------- | ------------------------ |
| 带素材  | `agentk.understand_media` | `gemini-3.1-pro-preview` |
| 纯文本  | `agentk.generate_text`    | `deepseek-v4-pro`        |

音频阶段仍走 `understand_media`，只是换成 `gemini-3.1-flash-lite`（见 §3.2）。

分流是必需的：`understand_media` 的 `mediaUrls` 是 `minItems=1`，纯文本环节送不进去。

→ **请确认**：用 `InvokeCapability` 直调而不是走 Session，对我们这种"服务内部把
AgentK 当模型后端"的场景是推荐用法吗？

---



## 3. 实测到的平台行为（请确认是否预期）

这一节是希望重点看的部分。以下都是实测结果，不是推测。

### 3.1 `understand_media` 不收音频，但错误被判为可重试，于是重试到超时

送音频素材会得到：

```
cannot determine a supported media type (declared "audio/mpeg");
supported: mp4/mov/webm/avi/flv/wmv/mpeg/3gp video and png/jpeg/webp/heic images
```

问题不在于不支持，而在于**这个错误被标成 Transient / retryable**，于是在服务端
重试到 capability 超时——实测 315~346 秒后才拿到这条错误，而结论从第一秒就已确定。
用户侧的体验是对着"运行中"等五分多钟，然后拿到一个提交那刻就注定的失败。

→ **请确认**：媒体类型不受支持属于永久性错误（invalid_argument），是否应该立刻返回
而不是重试？

### 3.2 `transcribe_audio` 在 PROD 上挂住不返回（我们已弃用这个能力）

**STAGING（2026-08）**：给 `audioUrl` 一个不存在的域名（`https://invalid.invalid/x.mp3`），
**60 秒后仍未返回**（我们本地取消）。同一段音频用 `data:audio/mpeg;base64,...` 内联送则
**5.0 秒返回**、转写正确。所以我们当时改成一律内联，不经上传。

**PROD（2026-09-03）**：同样的内联调用**也挂住了**——请求被接受、连接保持，直到我们
120 秒后取消。同一容器里 `understand_media` 与 `generate_text` 都正常（都用 KESS gRPC、
同一份凭证），只有这个能力不返回。

后果值得单说，因为它不止是"少一个能力"：转写失败没法缓存（缓存键是内容哈希，只存成功
值），而我们的 worker 路径会重放阶段函数，于是每一遍重放都重付一次满额超时。一次带参考
音频的质检里 4 × 120 = 480 秒，顶穿了该阶段 420 秒的 activity 预算，**整条质检失败**。

我们因此在 2026-09-03 把这个能力整条下线，音频改为一律走 `understand_media`
（音频阶段换 `gemini-3.1-flash-lite`，实测 8/8 真拿到音频）。代价是失去逐句时间轴。

→ **请确认**：`transcribe_audio` 在 PROD 环境是否有后端在跑？如果某个能力在某环境不可用，
调用方能否在**调用前**就发现（目录里查得到？），而不是靠一次挂满超时的调用？
→ 另外，拉取/处理失败是否应该有明确的超时并返回 `deadline_exceeded` / `unavailable`，
而不是一直保持连接？调用方拿"不返回"没法区分"慢"与"坏"。

### 3.3 `understand_media` 返回里没有 token 用量

返回只有 `text` / `model` / `params`，不含 usage。我们的报告因此把成本统计标成
`tokens_unavailable: true` 而不是 0，以免看起来像零成本。

→ **请确认**：是否有其他途径拿到这次调用的 token 用量（例如 metadata 或另一个接口）？

### 3.4 网关 300 秒同步上限与 capability 声明超时之间存在区间

`understand_media` 的 capability 超时是 360 秒，而网关对同步调用的容忍是 300 秒。
实测给一个不可达的媒体 URL，恰好 300.0 秒后回 `unavailable / Gateway Timeout`。
也就是 300~360 秒之间，能力还在跑、同步调用已被切断。

我们真实观测到的最长单次调用是主判阶段约 137 秒，有两倍余量，所以目前不是问题；
但更长的视频有可能逼近。传输层因此把这类失败单独措辞，不让它看起来像普通抖动。

**补充（改造后）**：worker 路径已改走 Temporal 原生的 `invoke_capability`，不经网关，
所以这条 300 秒上限只剩 studio 与批处理脚本受它约束。但原生路径**并不是没有上限**：
`internal/capability/mediaunderstand/manifest.go` 里 `activityStartToClose` 是
5 分钟 + 1 分钟 = 360 秒，而承载它的容器 workflow 直接把这个值当
`ScheduleToCloseTimeout` 用（`internal/pipeline/external.go`）。也就是说换成原生调用
只把天花板从 300 秒抬到 360 秒，净收益 60 秒。

→ **请确认**：360 秒是原生路径的真实上限吗？如果一次 `understand_media` 需要超过
360 秒（更长的视频、更多帧），调用方有办法调高吗，还是只能拆分调用？

→ **请确认**：走 `invoke_capability` 时，子 workflow 失败的错误文案与直调（Connect）
是否一致？我们现在按 §3.7 的办法解析文案里的 `(type: ..., retryable: ...)`，这套解析是拿
**直调**采到的样本验的，还没在 Temporal 路径上验证过。

### 3.7 同一个错误带两个互相矛盾的 `retryable` 标记，末尾那个恒为 `true`

对 STAGING 实调采到的四个样本（服务身份 `klingai-agentk-aigc-eval-api`，2026-08）
格式完全一致，都是嵌套两层分类：

```
invalid capability invocation: capability generate_text: model and prompt are required
  (type: InvalidArgument,        retryable: false)
: model and prompt are required (type: permanentActivityError, retryable: true)
: model and prompt are required
```


| 触发方式                              | 外层                                  | 内层                                        |
| --------------------------------- | ----------------------------------- | ----------------------------------------- |
| 调一个不存在的能力                         | `NotFound, retryable: false`        | `wrapError, retryable: true`              |
| `generate_text` 不给 model          | `InvalidArgument, retryable: false` | `permanentActivityError, retryable: true` |
| `generate_text` 给不存在的模型           | `Configuration, retryable: false`   | `permanentActivityError, retryable: true` |
| `understand_media` 给空 `mediaUrls` | `InvalidArgument, retryable: false` | `permanentActivityError, retryable: true` |


**四个都是不可能靠重试解决的永久错误**，外层的判断是对的；但内层一律标
`retryable: true`，连那个名字就叫 `permanentActivityError` 的也是。§3.1 里「媒体类型
不支持却被重试到超时」的机制大概率就在这里——如果平台内部的重试决策读的是内层标记，
那么每一个永久错误都会被一路重试到超时。

我们这侧的处理是**只认第一个标记**，并用真实样本钉成了回归测试
（`tests/test_error_classify.py`）。特别记一笔是因为最自然的实现——拿
`"retryable: true" in message` 做子串匹配——在这四个样本上**全错**，而错的代价是每个
永久错误都白发一次 `understand_media`，那是真花钱。

→ **请确认**：内层的 `retryable: true` 是有意为之，还是包装时丢了原始值？平台自己的
重试决策读的是哪一层？

→ **请确认**：外层的 `type` 是否有稳定的取值集合可以依赖（目前见到
`NotFound` / `InvalidArgument` / `Configuration`）？我们希望按它而不是按文案关键词分流。

### 3.5 能力目录的可见性与我们查到的不一致

浏览器上的能力目录页面显示 23 个能力；同一时刻我们用 `ListCapabilities`
（`ks-agentk-client`，服务身份 `klingai-agentk-aigc-eval-api`，STAGING）只拿到
20 个，且全部是 `source=CapabilitySource.CORE`，没有任何 `contrib.*`。

→ **请确认**：`ListCapabilities` 的返回是否与调用方身份/泳道相关？contrib 能力在
什么条件下会出现在这个列表里（是否只在同泳道、或需要 `include_variants`）？

### 3.6 文档站的能力目录页是空的

`/docs/capabilities` 这个页面通过文档 MCP 读取时正文为空（只有标题），
所以我们只能用 `ListCapabilities` 查线上目录来了解有哪些内置能力。
`/docs/worker/guides/deploy` 直接返回 500。

→ 如果有一份可读的内置能力清单（含输入输出 schema），麻烦指个位置。我们就是因为
没找到，一开始漏掉了 `transcribe_audio` 和 `extract_audio`，误判成"平台不能处理音频"。

---

## 4. 一个设计层面的问题：模型可能编造它拿不到的参数

`video_judge` 有两个必需输入：视频链接，以及**生成该视频所用的提示词**。后者只有
用户知道，模型拿不到。缺了它就无法判断"指令遵循"这一维。

我们在 profile 的 `system` 与 tool 的 `instructions` 里都写了"缺少提示词时先向用户
索取，不要自己编一个"。但这是期望，不是保证。**如果模型自己编一个填进去，报告会
照常产出、看起来完整，而"指令遵循"判的是模型编的要求——一个不报错、也不显示异常的
错误结论。**

我们为此在 `worker/verify_agentk.py` 加了 `--mode tool`：只发一句不含提示词的话，
把模型实际填的参数打出来并判定（编造判 FAIL，先索取判 PASS）。

→ **请确认**：平台侧有没有机制能更硬地保证这件事？比如：

- 让 capability 声明某个参数"必须来自用户、不允许模型推断"；
- 或者在 `ask_user_requested` 之外，有约定的方式让 tool 在参数不足时把问题抛回用户，
而不是让模型自行补齐。

如果没有，我们就只能靠提示词约束 + 输出侧标注"该维度未核实"来兜。

---

## 5. 部署计划（请确认是否正确）

目标是让别人的 agent 能调用这个能力，所以需要常驻部署到容器云。

我们的理解：

1. `worker.publish_workflow(...)` 本身就是发布，不需要额外去某处登记；
2. 能力在目录中的存在依赖 worker 持续刷新注册租约，worker 停了条目就消失；
3. 因此"发布"等于"把 worker 常驻起来"，形态是容器云的自定义服务。

镜像：`Dockerfile`，容器云 Python + uv 基础镜像，`kcsize` 作 1 号进程、
通过 `START_COMMAND` / `STOP_COMMAND` 托管 `deploy/container/start.sh`；
该脚本同时拉起 Temporal worker 与 HTTP 服务，任一退出即整体退出
（避免"worker 已死、能力已从目录消失，但健康检查仍绿"的半死状态）。
HTTP 侧监听平台分配的 `AUTO_PORT0`。

身份与配置：


| 变量                                                                  | 来源                 |
| ------------------------------------------------------------------- | ------------------ |
| `KWS_SERVICE_NAME` / `KWS_SERVICE_STAGE` / `KWS_SERVICE_CREDENTIAL` | 平台注入               |
| `KWS_LANE_ID`                                                       | **不设**，基线身份即轮询基础队列 |
| `AIGC_EVAL_BLOB_ENDPOINT` / `AIGC_EVAL_BLOB_BUCKET`                 | 部署单元里显式配置（不由平台注入）  |
| `TEMPORAL_ADDRESS`                                                  | **不设**。基线实例会主动忽略它  |
| `AGENTK_BASE_URL`                                                   | **不设**，身份由 KESS 提供 |


后两条各自对应一种难查的故障，所以我们在代码里做了防护：基线实例忽略
`TEMPORAL_ADDRESS`（否则残留这个变量会让 worker 静默连到空 namespace——启动正常、
日志干净、能力也 announce 了，但永远收不到任务）；基线实例缺 blob 配置直接拒绝启动、
退出码 2（否则 capability 能 announce、能接单，然后每次调用都在上传素材那步失败，
从调用方看是"能力在目录里、调过去就报错"）。

→ **请确认**：以上部署形态与变量约定是否正确？基线/泳道的判据用"有
`KWS_SERVICE_STAGE`、无 `KWS_LANE_ID`"是否准确？

---



## 6. 尚未验证的部分（如实标注）

以下都还没有真机验证过，因为 `connect_temporal()` 依赖机器上的 kess-agent，
而开发机（macOS）没有——本地起 worker 会在 KESS 发现 `klingai-muse-temporal-server`
时失败（`NotAvailableChannelException`，本地 kess-agent 连接被拒）。


| 项                                         | 状态                                       |
| ----------------------------------------- | ---------------------------------------- |
| worker 连上真实 Temporal                      | 未验证                                      |
| pipeline 路径端到端（`--mode pipeline`）         | 未验证                                      |
| tool 路径端到端（`--mode tool`），含第 4 节那个编造参数的风险 | 未验证                                      |
| 镜像构建                                      | 未验证（基础镜像在内部 registry，开发机无 podman/docker） |


已验证的是（本地 Temporal + 离线回放，不联网、不花模型钱）：


| 项                                                                                                                  | 怎么验的                                      |
| ------------------------------------------------------------------------------------------------------------------ | ----------------------------------------- |
| Capability 声明通过 SDK 校验、workflow 与八个 activity 注册并开始轮询、泳道路由生效（`contrib.aigc-eval@dev/<lane>`）、announce 打不通不拖死 worker | `tests/test_worker_boot.py`               |
| 八个阶段按序执行、状态逐段递进、进度经 signal 中继、命中报告缓存时只跑首尾两段                                                                        | `tests/test_worker_stage_workflow.py`     |
| **实例被 SIGKILL 后只从被杀那一段重跑**，前面各段不重跑                                                                                 | `tests/test_worker_kill_resume.py`        |
| 确定性失败只跑一次、可重试失败重跑一次（`maximum_attempts=2` 生效）                                                                       | `tests/test_worker_retry.py`              |
| 阶段化与单体两条路径产出同一份报告、同一串进度事件、同样的 trace 步数与成本                                                                          | `tests/test_worker_stages.py`（模型响应取自真实录制） |
| 阶段状态跨实例可读、键不互相覆盖、读写失败一律抛而不静默                                                                                       | `tests/test_stage_state.py`               |


调用方那一侧（`understand_media` / `generate_text`）在 STAGING 与 PROD 上都真机跑通了
（PROD 用 `deploy/container/smoke_check.py` 验的）。

### 我们打算怎么验

直接在容器云上部署一个**非基线泳道实例**联调：部署单元里显式给
`KWS_LANE_ID=dev/<用户名>`，worker 于是注册在 `contrib.aigc-eval@dev/<用户名>`，
只接同泳道流量，不碰基线队列。验完再去掉 `KWS_LANE_ID` 转基线。

调用侧仍从开发机发起（`worker/verify_agentk.py`），两点是我们查证后确认的：

- 客户端默认走 **HTTP transport**，按 `KWS_SERVICE_STAGE` 推导网关、不需要本机
kess-agent。实测这条 transport 支持完整 RPC 面，包括流式 `watch_events`
（对不存在的 execution 明确回 `execution stream not found`）；
- 泳道由调用方声明：`request_lane.use("dev/<用户名>")`，SDK 以 `agentk-lane-id`
头带出，注入在两个 transport 共用的 `client/_core.py` 里。所以脚本加了 `--lane`。
不带这个头的请求走基线队列——表现是执行一直排队、没有任何进度事件，看着像 worker
没起来，其实是打到了另一条队列上。

→ **请确认**：这个联调形态对吗？特别是——泳道实例的 `KWS_LANE_ID` 与调用方的
`agentk-lane-id` 取同一个值，是否就是同泳道路由的正确用法？另外泳道实例在
**能力目录**里是否可见（第 3.5 节那个 20/23 的差异可能与此相关）？

联调时打算顺手验的一件事：泳道实例只起**一个**副本时，杀掉它再拉起，一次在途的质检
应当从被杀的那一段接着跑（本机已验，真机想再确认一遍，因为那里还牵涉容器重启后
对象存储中的阶段状态与产物是否都还在）。