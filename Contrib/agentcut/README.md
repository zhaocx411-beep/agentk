# AgentCut

AgentCut 是基于 Agent-K 0.3.2、Temporal 和 React 的对话式视频剪辑器。Timeline
Workflow 是项目状态的权威来源；Agent-K 负责 Session、Execution 和事件流；
AgentCut Worker 发布 `contrib.agentcut.*` capability 并执行时间线工具。

## 日常开发：本地进程 + 个人 staging 泳道

当前 Agent-K mainline 会拒绝 `AGENTK_MODE=standalone`，因此不再支持旧的
五进程本地闭环。本地 AgentCut 直接通过 KESS 使用 staging 的 Agent-K API 与
Temporal；媒体和 Timeline 快照写入 staging BS3。每位开发者必须把下面的
`yourname` 替换为自己的短标识，使用个人 lane、内部队列和用户；禁止笔记本长期
发布 baseline Worker。

```bash
# 三个终端共用；不要设置 TEMPORAL_ADDRESS 或 AGENTK_BASE_URL
export AGENTCUT_DEVELOPER=yourname
export KWS_SERVICE_STAGE=STAGING
export KWS_SERVICE_NAME=klingai-agentk-agentcut-api
unset KWS_SERVICE_CREDENTIAL
export KWS_LANE_ID="dev_${AGENTCUT_DEVELOPER}"
export FALLBACK_TO_ENV_LANE_ID=true
export AGENTCUT_TASK_QUEUE="contrib.agentcut.internal.dev_${AGENTCUT_DEVELOPER}"
export AGENTCUT_AUTH_MODE=dev
export AGENTCUT_DEV_USER="${AGENTCUT_DEVELOPER}"
export AGENTCUT_BLOB_BACKEND=blob
export AGENTCUT_BLOB_ENDPOINT=https://bs3-hb1.staging.kuaishou.com
export AGENTCUT_BLOB_BUCKET=mmu-busi-lingo
unset AGENTK_BASE_URL
unset TEMPORAL_ADDRESS
unset TEMPORAL_NAMESPACE

# AgentCut API（终端 1）
go run ./cmd/apiserver
```

```bash
# AgentCut Worker（终端 2，先设置上面的共用环境变量）
export AGENTCUT_RUN_INTERNAL_WORKER=true
go run ./cmd/worker
```

```bash
# Web（终端 3；Vite 环境变量必须在启动前设置）
cd agentcut-web
export VITE_AGENTCUT_USER="${AGENTCUT_DEVELOPER}"
npm install
npm run dev
```

本机必须有可用的 KESS dev agent，`klingai-agentk-agentcut-api` 必须已经登记并
绑定到 Agent-K App。API 和 Worker 启动时都会调用 `WhoAmI`；任何身份或服务发现
错误都会在接受业务流量前失败。Worker 应同时显示个人内部队列和
`contrib.agentcut.capability@dev_<yourname>`。

只做手工 Timeline 开发时显式设置 `AGENTCUT_AGENTK_DISABLED=true`。API 会以
manual-only 模式启动，项目不会绑定 Agent-K Session，对话接口返回 503，但上传、
时间线编辑和渲染仍可用。Worker 同样只跳过 Agent-K 资产校验，不阻断现有
Timeline Workflow。未设置 `AGENTK_BASE_URL` 不再表示禁用 Agent-K。

## Staging / KESS

AgentCut 缺省使用 Agent-K SDK 0.3.2 的 gRPC/KESS 服务发现连接
`klingai-agentk-api`。`AGENTK_BASE_URL` 仅用于本地联调或测试 endpoint 覆盖，
开发环境不需要设置。SDK 在普通 unary RPC 和 `WatchEvents` stream 上发送同一
KESS identity：

```bash
export KWS_SERVICE_NAME=klingai-agentk-agentcut-api
export KWS_SERVICE_STAGE=STAGING
unset AGENTK_BASE_URL
unset KWS_SERVICE_CREDENTIAL  # 开发环境无需 credential
unset AGENTK_API_KEY

# staging 必须走 agentk-sdk 的 KESS Temporal 发现，避免误连本地集群。
unset TEMPORAL_ADDRESS
unset TEMPORAL_NAMESPACE
```

这组连接参数不负责选择 Worker lane；日常开发仍须保留上一节设置的个人
`KWS_LANE_ID`。只有受控 baseline 部署或验证才清空该变量。

`KWS_SERVICE_NAME` 被编码到 `kws-bin`，credential 发送到 `kwsCredential`。
开发环境允许仅用已登记并绑定 App 的服务名；生产环境由平台注入
`KWS_SERVICE_CREDENTIAL`。环境变量在进程创建客户端时读取，修改后需重启 API
和 Worker。

`AGENTK_API_KEY` 已废弃。兼容代码只读取它以打印 warning，绝不会发送
`Authorization: Bearer ...`。不要把 API Key 当作 KESS credential。

## Request Lane 与 Worker 队列

AgentCut API 在 HTTP 边界读取公司 `trace-context` 的 `laneId`，并把它写入
Agent-K request context。SDK 会自动把该值传播到 Agent-K RPC 和 Temporal header。

Worker 使用两条逻辑队列：

- `contrib.agentcut`：内部 Timeline、render、prepare Workflow/Activity，只由
  baseline Worker 消费；
- `contrib.agentcut.capability`：发布给 Agent-K 的工具能力。baseline 实例监听
  原始队列，设置 `KWS_LANE_ID=<lane>` 的实例监听
  `contrib.agentcut.capability@<lane>`。

named-lane Worker 默认只启动 capability Worker；日常开发需要同时运行个人内部
Worker，因此必须显式 opt-in 并设置非共享内部队列：

```bash
export AGENTCUT_DEVELOPER=yourname
export KWS_SERVICE_STAGE=STAGING
export KWS_SERVICE_NAME=klingai-agentk-agentcut-api
export KWS_LANE_ID="dev_${AGENTCUT_DEVELOPER}"
export AGENTCUT_TASK_QUEUE="contrib.agentcut.internal.dev_${AGENTCUT_DEVELOPER}"
export AGENTCUT_RUN_INTERNAL_WORKER=true
go run ./cmd/worker
```

未设置 `AGENTCUT_RUN_INTERNAL_WORKER` 时，baseline 自动运行内部 Worker，named
lane 不运行；显式 `false` 可关闭。named lane 设置 `true` 时若未显式提供个人
`AGENTCUT_TASK_QUEUE`，或队列为 `contrib.agentcut` /
`contrib.agentcut.capability`，Worker 会拒绝启动，避免争抢共享任务。若本地 API
是请求源、上游没有注入 `trace-context`，可设置 `FALLBACK_TO_ENV_LANE_ID=true`
和相同的 `KWS_LANE_ID`；否则不要启用 fallback。
缺少 `KWS_SERVICE_STAGE` 且未显式设置 `KWS_LANE_ID` 会被拒绝，避免 SDK 自动进入
`local-*` lane 后任务落到无人消费的队列。

受控 baseline 验证必须清空 `KWS_LANE_ID` 和 fallback，并使用正式共享内部队列；
验证结束后立即停止。生产部署不设置 `AGENTCUT_RUN_INTERNAL_WORKER`，baseline
行为与升级前兼容；认证使用 `trusted-header`，不会读取 `AGENTCUT_DEV_USER`。

## Capability manifest 与 Profile

- `cmd/worker` 使用 Agent-K Worker SDK 0.3.2。`tools.Definitions()` 中的声明会在
  Worker 启动时组成 capability manifest，发布 `contrib.agentcut.*` 并周期续约。
- `agents/agentcut/profile.json` 是创建 Session 时提交的 v0.3 Profile；
  `agents/agentcut/meta.json` 是 AgentCut 展示元数据。Profile 中的 capability ID
  必须与 Worker 发布的 manifest 完全一致。
- API 为每个项目复制 Profile，并向 timeline 工具的 `toolDefaults` 注入
  `projectId`。Profile 含未知旧字段时会在创建 Session 前失败，不会静默降级。
- Worker 与 Agent-K 必须连接同一 Temporal 集群；否则 Profile 可以创建，但
  `contrib.agentcut.*` 不在线或调用超时。

## WhoAmI 启动失败排查

- KESS discovery 失败：检查本机 KESS dev agent，以及
  `klingai-agentk-api` 的开发环境服务发现。
- `connection refused`（仅 endpoint 覆盖）：检查 `AGENTK_BASE_URL`、Agent-K
  监听端口和协议。
- `Unauthenticated`：开发环境检查 `KWS_SERVICE_NAME` 是否已登记并绑定 App；
  生产环境还需检查平台是否注入 credential。不要设置 `AGENTK_API_KEY` 代替它。
- `authenticated identity has no app id`：endpoint 不是 Agent-K 0.3.2，或身份没有
  解析到 App。正常日志会显示 `appId` 和 `displayName`。
- Worker capability 不在线：确认 Worker 未退出、Temporal/KESS 指向同一集群，
  并从启动日志核对 `base_task_queue`、物理 `task_queue` 和 lane。

## 验证

```bash
go fmt ./...
go test ./...
go test -race ./...
go vet ./...

cd agentcut-web
npm test
npm run lint
npm run build
```

先用独立 canary 验证 staging BS3 的 PUT、GET、Range GET 和 DELETE；对象只写入
`agentcut-e2e/<user>/...` 并自动清理：

```bash
export AGENTCUT_DEVELOPER=yourname
export AGENTCUT_DEV_USER="${AGENTCUT_DEVELOPER}"
export AGENTCUT_BLOB_BACKEND=blob
export AGENTCUT_BLOB_ENDPOINT=https://bs3-hb1.staging.kuaishou.com
export AGENTCUT_BLOB_BUCKET=mmu-busi-lingo
go run ./cmd/blobcheck
```

最小 Agent E2E 不上传、不渲染。API 需额外设置
`AGENTCUT_E2E_PROFILE=true`，API/Worker 按个人泳道配置运行，再执行：

```bash
export AGENTCUT_E2E_ALLOW_STAGING_WRITES=true
export AGENTCUT_API_URL=http://127.0.0.1:8080
go run ./cmd/agente2e
```

Runner 会自建项目和 Agent-K Session，验证 SSE、Events 以及唯一一次
`timeline_get`，随后删除项目。Agent-K 暂无 DeleteSession，日志会输出留下的空闲
Session ID 供审计。缺少模型、KESS、个人 lane capability 或 opt-in Profile 时，
Runner 会在对应阶段失败；媒体 prepare/render 继续使用单测和
`cmd/rendersmoke` 做受控验收。
