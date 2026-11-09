# Changelog

Python SDK 的版本记录，覆盖 `ks-agentk-client`、`ks-agentk-worker` 和
`ks-agentk-protocol`，对应 Git tag `python/vX.Y.Z`。

## python/v0.4.2 - 2026-09-15

### 修复

- 升级 `ks-resolve` 依赖，带入上游对跨区域服务发现的 bug 修复。

## python/v0.4.1 - 2026-09-13

### 调整

- 使用 gRPC 客户端或 Worker 时，可以安装 `ks-resolve` 0.1.0 及以上版本，
  不再要求固定版本，便于与应用的其他依赖一起升级。

## python/v0.4.0 - 2026-09-13

### 升级注意

- 使用此版本需要 Python 3.12 或更高版本。
- gRPC 客户端和 Worker 改用 `ks-resolve`，不再引入 `grpcio`、`infra-*`、
  `dsc-auth` 和 `ks-kafka-python` 等依赖。
- 设置 gRPC 请求泳道请改用 `ksresolve.rpc.with_lane`；移除 `ks-agentk-request-lane`
  包及其 `agentk.request_lane` 导入入口。客户端不再发送 `agentk-lane-id`，
  也不再根据 PRT 环境自动设置请求泳道。
- 部署在平台上的 Worker 按 `KWS_LANE_ID` 原值选择泳道；未配置时使用默认泳道，
  PRT 环境也不再自动补为 `prt`。本地运行时仍会按用户和主机生成隔离泳道。

### 新增

- 能力调用响应新增 `invocation_id` 字段，用于标识本次调用。
- Workflow 和 Activity 可以继续传递发起请求的泳道，Activity 中的 ksresolve 调用会沿用该泳道。

### 修复

- 修复使用默认 Workflow 运行器时，Workflow 模块导入 `agentk.worker` 可能出现的
  `invalid field descriptor` 错误。

## python/v0.3.7 - 2026-09-09

## python/v0.3.6 - 2026-08-31

## python/v0.3.5 - 2026-08-28

## python/v0.3.2 - 2026-08-26

## python/v0.3.1 - 2026-08-25

## python/v0.3.0 - 2026-08-24

## python/v0.2.2 - 2026-08-18

## python/v0.2.1 - 2026-08-18
