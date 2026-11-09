# Changelog

Java SDK 的版本记录，Maven groupId 为 `com.kuaishou.klingai.agentk`，
对应 Git tag `java/vX.Y.Z`。

## Unreleased

### 升级注意

- 客户端不再设置请求泳道，PRT 环境也不再自动使用 `prt`。
  通过 KESS 发现服务时，仅连接未设置泳道的 AgentK 实例。
- 移除 `agentk-request-lane`、`agentk-request-lane-grpc` 和
  `agentk-request-lane-servlet-jakarta` 模块。升级时需移除这些依赖，以及
  `KsRequestLaneServerInterceptor`、`KsRequestLaneFilter` 和 `RequestLane` 的使用；
  SDK 不再提供对应的请求泳道功能。
- 移除生成的 `CapabilityManifest`、`Capability`、`ToolRegistration`、`CapabilityKind`、
  `InvocationMeta` 和 `ReportProgressRequest` 类型；直接引用这些类型的代码需要调整。

### 新增

- 能力调用响应新增 `invocation_id` 字段，可通过 `getInvocationId()` 读取本次调用的标识。

## java/v0.3.6 - 2026-08-31

### 新增

- 发起 Agent 消息执行时，可以通过 `attachments` 提交图片附件；新增 `MediaAttachment`
  类型描述附件地址和媒体类型，用户输入事件中也可读取这些附件。

## java/v0.3.5 - 2026-08-28

## java/v0.3.2 - 2026-08-26

## java/v0.3.1 - 2026-08-25

## java/v0.3.0 - 2026-08-24

## java/v0.2.2 - 2026-08-18

## java/v0.2.1 - 2026-08-18

## java/v0.2.0 - 2026-08-18
