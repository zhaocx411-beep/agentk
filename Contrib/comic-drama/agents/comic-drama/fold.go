// 会话事件账本 → 阶段进度的折叠。
//
// GetExecution 只给 exec 级状态与最终 output；节点级进度落在
// 会话事件账本的 invocation.progress 事件里（worker 每完成一个阶段汇报一条）。
// 这里按事件序增量扫描，把属于指定 exec 的进度事件投影成 ProgressStep 列表。
package comicdrama

import (
	"encoding/json"
	"time"

	"studio/platform/core"
)

// foldProgress 从 events 中挑出属于 execID 的 pipeline invocation.progress
// 事件，追加到 progress 之后返回。events 必须按事件时间正序排列。
func foldProgress(progress []ProgressStep, execID string, events []core.LedgerEvent) []ProgressStep {
	for _, ev := range events {
		if ev.Kind != "invocation.progress" || ev.ExecutionID != execID {
			continue
		}
		var payload struct {
			InvocationKind string `json:"invocationKind"`
			StepID         string `json:"stepId"`
			Message        string `json:"message"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			continue
		}
		if payload.InvocationKind != "pipeline" || payload.StepID == "" {
			continue
		}
		at := ev.CreatedAt
		if at.IsZero() {
			at = time.Now()
		}
		progress = append(progress, ProgressStep{
			StepID:  payload.StepID,
			Message: payload.Message,
			At:      at,
		})
	}
	return progress
}
