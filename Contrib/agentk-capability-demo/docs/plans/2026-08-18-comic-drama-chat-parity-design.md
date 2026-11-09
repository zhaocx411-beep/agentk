# Comic-Drama Chat 交互对齐设计

## 目标

Agent Loop 保持 Python SDK 直连 AgentK，同时复现 `comic-drama/platform/chat` 的关键
交互：多轮 Session、执行锁、工具与产物、`ask_user` 问答，以及本地素材上传。

## 状态与数据流

前端以 AgentK ledger 为唯一事实来源。每次轮询会从完整事件序列折叠用户消息、
assistant 文本与 reasoning、工具状态、产物和待回答问题。`askUserRequested` 按
`toolCallId` 加入待回答集合，`askUserResponded` 将其移除；提交答案后的短暂最终一致性
窗口用本地 `answeredAsks` 集合抑制旧问题卡再次出现。

消息发送、文件上传、Agent execution 和待回答问题共用 composer 锁。任一状态活跃时，
文本框、文件选择器和发送按钮均禁用；取消或终态后恢复。

## Ask User

每个问题同时支持 schema 选项和“其他答案”。单选的自定义答案覆盖已选项，多选的
自定义答案追加到选项。所有问题必须有答案才允许提交。答案通过独立 answer execution
发送，继续同一 AgentK Session，而不是伪装成新的普通消息。

## 上传

文件选中或拖入后立即进入本地上传队列，显示上传中、已上传或失败状态；图片显示
缩略图。上传成功返回名称、媒体类型、MIME、大小和 URL。当前 Python SDK v0.2.2 的
`AgentMessageExecutionInput` 只有 `text`，所以所有素材按
`文件名（类型）：可访问 URL` 追加到消息，和 comic-drama Chat 的实现保持一致。

## 验证

- Python API 测试验证上传分类、附件消息格式与 answer execution。
- Ruff、pytest、`node --check` 和 `git diff --check` 作为提交门禁。
- 浏览器真实触发双问题 `ask_user`，提交回答并确认下一轮恢复。
- 使用真实 PNG 调用上传接口，确认 image 类型、MIME、大小和 URL。
