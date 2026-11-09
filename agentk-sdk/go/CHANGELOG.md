# Changelog

Go SDK 的版本记录，对应 Git tag `go/vX.Y.Z`。

## go/v0.5.1 - 2026-09-15

### 升级注意

- Temporal Go SDK 依赖从 v1.41.1 升至 v1.46.0，`contrib/opentelemetry` 升至 v0.8.1。
- Temporal SDK 计数类指标改用 OpenTelemetry monotonic counter 上报，导出端会把它们识别为
  counter 而不是 up-down sum。

### 修复

- 升级 ksresolve 依赖，带入上游对跨区域服务发现的 bug 修复。

## go/v0.5.0 - 2026-09-13

### 升级注意

- 设置 gRPC 请求泳道请改用 ksresolve 的 `rpc.WithLane`；移除 `requestlane` 和
  `requestlane/ksingress` 包。客户端不再发送 `agentk-lane-id`，也不再根据 PRT
  环境自动设置请求泳道。
- 部署在平台上的 Worker 按 `KWS_LANE_ID` 原值选择泳道；未配置时使用默认泳道，
  PRT 环境也不再自动补为 `prt`。本地运行时仍会按用户和主机生成隔离泳道。
- `worker.InstanceLaneFromEnv()` 的返回类型从 `(requestlane.Lane, error)` 改为
  `(string, error)`；使用显式类型声明的代码需要相应调整。原先通过 `requestlane.Parse`
  校验泳道的代码请改用 ksresolve 的 `rpc.ValidateLane`。
- 协议类型和生成的客户端移至独立 Go module
  `git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen`，import 路径保持不变。
  本版本依赖该 module 的 v0.1.1；只使用协议类型的应用可以直接依赖它，无需引入完整 SDK。

### 新增

- 能力调用响应新增 `InvocationId` 字段，用于标识本次调用。
- Workflow 和 Activity 可以继续传递发起请求的泳道，Activity 中的 ksresolve 调用会沿用该泳道。
  自行创建 Temporal 客户端时可使用 `temporal.CarrierPropagator`；Workflow 中可通过
  `temporal.LaneFromWorkflow` 读取泳道，通过 `temporal.CarrierFromWorkflow` 读取完整追踪上下文。
- 新增 `worker.TaskQueueForLane(baseTaskQueue, lane)`，可获取指定泳道对应的 Temporal 任务队列名称。
- 通过 SDK 连接 Temporal 时，启用 Temporal 客户端内置运行指标。

## go/v0.4.2 - 2026-09-07

## go/v0.4.1 - 2026-09-06

## go/v0.3.6 - 2026-09-01

## go/v0.3.5 - 2026-08-28

## go/v0.3.2 - 2026-08-26

## go/v0.3.1 - 2026-08-25

## go/v0.3.0 - 2026-08-24

## go/v0.2.2 - 2026-08-18

## go/v0.2.1 - 2026-08-18

## go/v0.2.0 - 2026-08-17

## go/v0.1.4 - 2026-08-13

## go/v0.1.3 - 2026-08-13

## go/v0.1.2 - 2026-08-11

## go/v0.1.1 - 2026-08-11

## go/v0.1.0 - 2026-08-10
