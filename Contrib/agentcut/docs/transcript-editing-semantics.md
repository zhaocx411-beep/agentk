# 文字稿驱动剪辑语义

状态：M2 后端契约已实现。

## 结论

MVP 不建议新增底层 Timeline op。用户删除文字应先被编译成一个或多个时间线区间，再以**从后向前**的 `rippleDelete` 操作组成同一个 `ApplyOps` 批次提交。

`trimStart/trimEnd` 只适合 item 边缘且不会自动保持多轨同步；`removeItems` 只适合完整 item。现有 `rippleDelete` 已能处理区间内完整 item、边缘裁剪、跨区间 item 分裂以及后续内容左移，最符合“删字即删时间”的语义。

## 数据语义

1. `Media.Transcript` 是源素材时间轴上的 ASR 原稿，`startMs/endMs` 使用源媒体时间，不随项目剪辑改写。
2. 文字稿编辑页展示的是从当前 Timeline item 派生的“项目文字稿视图”，不是直接修改源原稿。
3. `setTranscript` 只用于 ASR 导入/整体替换，不能表示用户删字。
4. `setCaptions` 只控制呈现字幕，也不能作为剪辑事实来源。
5. `TranscriptSegment` 和 `TranscriptWord` 需要增加稳定的 `segmentId/wordId`；前端不能用文字内容或数组下标标识选择范围。

## 删除编译流程

前端提交高层命令，而不是自行计算 op。先读取：

`GET /v1/projects/{projectID}/transcript-view`

响应中的 `occurrenceId` 是项目、Timeline revision、素材 generation、
Transcript revision 和文字条目的不透明选择标识。之后提交：

```json
{
  "clientMutationId": "mutation-123",
  "baseRevision": 12,
  "ranges": [{
    "fromOccurrenceId": "occ1...",
    "toOccurrenceId": "occ1..."
  }],
  "mode": "ripple"
}
```

到 `POST /v1/projects/{projectID}/transcript-edits:delete`。后端在同一个
TimelineEntityWorkflow Update 内：

1. 把词的源时间区间和每个 item 的源区间求交。
2. 按 item 的 `trimStart`、`speed` 和 Timeline `fps` 映射成时间线帧区间。
3. 合并相邻/重叠区间并按时间从后向前排序，避免前一次 ripple 改变后续坐标。
4. 生成 `rippleDelete{fromFrame,toFrame}`；默认作用于所有未锁定轨道，以保持画面、原声、配音和字幕同步。
5. 作为单个 `ApplyOps` 批次提交，任一范围校验失败则整批失败。

源毫秒映射时，开始帧向下取整、结束帧向上取整，最终 clamp 到 item
的 `[from, end)`。客户端和 Agent 都不得提交自行计算的帧区间。

同一个源素材被多个 item 使用时，每个可见实例都要单独映射；跨多个 item 的选择最终会得到多个区间。若区间在时间线上不连续，不能把中间内容一起删除。

## 锁定轨和编辑模式

MVP 只提供“全局 ripple”模式：

- 任一锁定轨上的 item 只要会被删除区间覆盖或被后续 ripple 左移，
  返回 `LOCKED_TRACK_CONFLICT`，Timeline 完全不变。
- 编译后的 op 显式列出所有未锁定轨，不依赖底层空 `trackIds` 的跳过语义。
- “仅删除文字但保留空隙”和“仅编辑某一轨”后续可分别编译为 split/trim/remove 组合，不应改变 MVP 默认语义。

## API 与实现影响面

- `GET /v1/projects/{id}/transcript-view`
- `POST /v1/projects/{id}/transcript-edits:delete`
- Agent-K capability `contrib.agentcut.timeline_edit_transcript`
- 稳定错误码：
  `INVALID_OCCURRENCE_ID`、`CROSS_PROJECT_OCCURRENCE`、
  `STALE_TRANSCRIPT_SELECTION`、`TRANSCRIPT_WORD_NOT_FOUND`、
  `TRANSCRIPT_SEGMENT_NOT_FOUND`、`TRANSCRIPT_TIMESTAMP_UNAVAILABLE`、
  `LOCKED_TRACK_CONFLICT`

相同 `clientMutationId` 仅可重试完全相同的网络请求。409 或 422 后，
修正后的请求必须使用新的 ID。

## 关键风险

- 毫秒到帧的舍入必须固定规则，否则连续编辑会产生一帧重叠或空洞
- 变速、重复使用素材、无词级时间戳需要明确降级策略
- 当前模型缺少 item/link group；MVP 以所有轨严格同步 ripple 处理
- ASR 原稿更新和用户编辑并发时必须以 transcript revision 拒绝旧选择，不能静默套用
