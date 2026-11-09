# Narrato 与 AgentK 平台集成对齐设计

## 目标

审计 Narrato 纪录片解说 Pipeline 与 AgentK 平台的集成方式，纠正其中偏离 Worker SDK 公开契约
的部分，并沉淀出一层可复用的 Agent 100 集成框架。这是新增第二个 Agent 100 workflow agent 的
前置条件：当前每一个平台接触点都是按 Narrato 内联写死的，要做兄弟 agent 只能整棵模块树复制。

本文只覆盖集成面。解说内容质量、节奏参数开放、脚本编辑器，以及 v1/v2 收敛另行立项。

## 现状：六个平台接触点

Narrato 与 AgentK 有六处接触，其中四处已经符合平台契约，应当保留并作为后续 agent 的模板。

**Capability 发布方式正确。** `worker.py` 先用 Temporal 原生 API 注册三个 workflow，再调用
`publish_workflow` 传入 id、`temporal_type`、说明、输入输出 JSON Schema 和超时，与《实现并发布
Capability》中的发布模型一致。Capability id 使用 `contrib.narrato.*` 命名空间，Temporal Type
带显式 `.v2` 后缀，公开契约与部署细节各自独立演进。

**业务输入是干净的。** 三个 workflow 只接收领域参数，`GENERATE_INPUT_SCHEMA`、
`RENDER_INPUT_SCHEMA`、`CREATE_INPUT_SCHEMA` 里没有混入 session、execution 或回调地址。执行
坐标按平台设计由 Worker SDK 的 invocation context 传递。

**消费侧接入正确。** `service.py` 创建 session 时用 `Profile` 声明三条 pipeline，只写 capability
id 加会话内名称，再用 `PipelineExecutionInput` 调 `StartExecution`。Profile 不承载 task queue 和
Workflow Type。

**启动期契约校验强于平台基线，应当保留并提升。** `validate_core_capabilities` 会拉取 catalog，
校验依赖的 Core capability 全部已发布、每个 capability 仍然暴露 Narrato 依赖的输入输出属性、
配置的模型名仍出现在 capability 的 `model` enum 中。这是一道针对上游 schema 漂移的真实防线，
SDK 本身没有等价能力。

## 偏离项

### Core 能力被当作裸 Activity 调用

Narrato 通过 `workflow.execute_activity` 调用 Core，Activity Type 硬编码在 `CoreTemporalTypes`，
`task_queue` 显式钉死。Worker SDK 为此专门导出了
`invoke_capability(capability_id, input, result_type=...)`，《实现并发布 Capability》的发布检查
明确要求 workflow 按稳定 capability id 调用其他能力，**不复制目标 task queue 和 Temporal Type**。
Narrato 只从 `agentk.worker` 导入了 `report_progress`，`invoke_capability` 在整个项目中未出现。

由此产生四个后果。其一，绕过 Request Lane 路由：平台按 capability 逐次解析 task queue，泳道
请求得以命中泳道 Worker 并在未覆盖处回落基线，而钉死队列使所有调用恒定落在 baseline，抵消了
客户端侧已经做好的泳道支持。其二，隐含假定每个 Core capability 都以 Activity 实现，平台一旦把
某个能力改为 Workflow 实现，Narrato 会静默失效。其三，失去 `result_type` 的类型化反序列化，
只能靠 `first_artifact_uri`、`text_result` 手工解包。其四，为了让 Temporal workflow sandbox 解析
到同一个队列，被迫把 `NARRATO_CORE_TASK_QUEUE` 镜像进进程环境变量。

切换前必须先测两件事。`invoke_capability` 会把每次调用变成 `agentk.capability.invoke.v1` 薄壳
child workflow，增加 history 事件；一次长视频 Narrato 运行会发起数百次 Core 调用，history 增长
需要对照 Temporal 限制量化。另外 `agentk.concat_videos` 注册为 Core Activity 但刻意不进消费侧
catalog，薄壳能否按 id 解析到它，必须在移除裸调用路径前验证。

### 进度事件被当作请求日志使用

`_report_request` 和 `_report_response` 把每次 Core 调用的完整请求体和完整响应推进
`report_progress`。在策划和脚本阶段，请求体就是整段 prompt，其中内嵌了全片的序列化证据。
《能力执行与进度回传》明确指出进度是业务阶段事件，且不应把日志当作产品进度。这一做法既撑大了
Session Event Log，也正是 console 每次轮询都必须翻完整个 session 的事件再在客户端过滤的根因。

### 用百分比顶替了步骤身份

进度百分比是硬编码魔数（5、10、22、26、52、62、78、98），承载在 progress 的 `data` 中，而整个
扇出阶段共用同一个 `step_id`。平台把 `step_id` 定义为稳定的业务步骤，并明确说明它不是百分比，
建议并行步骤使用 `clip/0`、`clip/1` 这类稳定位置标识。由于所有分析窗口共用一个 `step_id`，下游
无法判断哪个窗口正在执行，界面也只能渲染一个字符串和一根进度条。

### 进度上报失败会导致整个任务失败

所有 `report_progress` 都是裸 await，没有任何保护。平台指引是进度回传属于可选旁路，默认不应
让昂贵流程失败。一次数小时的渲染因为一个进度事件上报失败而中止，代价不成比例。

### Worker 连接方式与队列命名

`connect_worker_temporal` 优先使用显式 Temporal 地址和 namespace，失败才回落到 SDK 的
`connect_temporal()`。发布检查要求不自行配置地址和 namespace。考虑到本地 standalone 的内嵌
Temporal 端口是随机分配的，这个显式分支确有必要，因此建议保留但收窄为有文档说明的本地专用
路径。另外当前 task queue 名为 `demo-worker`，而指南要求 contributor Worker 使用自己命名空间下
的队列，例如 `contrib.narrato-worker`。

## 拟引入的集成框架层

抽出 `agent100/framework/`，承载与 Narrato 业务无关的平台机制：

`CoreCapabilityClient` 封装 `invoke_capability`，内聚现有的各类 retry policy 以及
`_is_degradable_activity_error` 中的可降级错误判定。

`StageReporter` 持有声明式的阶段定义，由阶段权重推导百分比，为扇出工作发出位置化 `step_id`，
并保证任何上报失败都不向外传播。

`ModelOutputResolver` 实现"生成 → 校验 → 带错误修复重生成"的循环，目前策划阶段和脚本阶段各自
重复实现了一遍。

以及从 `worker.py` 提取出的 catalog 契约校验。

同时把 `Agent100Definition` 从展示元数据升级为真正的插件契约，额外提供 router、settings 加载器、
pipeline 声明和 worker 工厂，使第二个 agent 只需实现 contracts、prompts 和 stages。

请求账本（request ledger）对测试台确有价值，予以保留，但从进度通道移到 Worker 侧的独立调试
汇聚点，由 console 单独读取，使 Session Event Log 只保留业务阶段。

## 迁移路径

Core 调用方式的改动会改变 activity 调度，不是 replay-safe 的，因此以 `.v3` 新 Temporal Type
落地，而不是再加 patch flag。这同时是清理四个历史 patch flag 的时机：在新类型中把
`opening-cadence`、`positioning-hook`、`video-preclip`、`post-event-narration` 全部固化为 true，
删除它们在提示词中分叉出的分支——目前同一段节奏指导已经因此存在三个变体。

执行顺序：先量化 child workflow 的 history 开销并确认 `concat_videos` 的解析行为；再以现有裸
Activity 实现为内核抽出 `CoreCapabilityClient`，保证这一步行为不变；然后在新 workflow type 下
把内核切换为 `invoke_capability`；最后把请求账本移出进度通道。

## 验证

单元测试覆盖框架层本身：阶段权重到百分比的推导、位置化 step id、进度上报失败被吞掉、可降级
错误判定，以及模型输出修复循环。现有的 Narrato contracts、workflows 和 worker 测试必须在接入
抽出的 client 后原样通过，以证明抽取过程行为不变。

集成验证需要在线的 standalone AgentK、Narrato Worker 和 Core Worker。确认三个 capability 仍以
不变的 schema 出现在 `ListCapabilities` 中；用一段短视频跑通 `documentary_video` 全流程；对比
切换 `invoke_capability` 前后单次运行的 Temporal history 事件数。泳道路由的验证方式是：为某一个
Core capability 启动一个泳道 Worker，确认带泳道标识的 execution 命中它，而不带标识的 execution
仍然留在 baseline。
