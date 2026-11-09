package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"agentk/internal/event"
	"agentk/internal/types"
)

func ProfileVersion(profile []byte) string {
	sum := sha256.Sum256(profile)
	return hex.EncodeToString(sum[:6])
}

func RoundID(executionID types.ExecutionID, round int) event.RoundID {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", executionID, round)))
	return event.RoundID("rnd_" + hex.EncodeToString(sum[:])[:24])
}

func BatchID(executionID types.ExecutionID, slot string) event.BatchID {
	return event.BatchID(fmt.Sprintf("%s/%s", executionID, slot))
}

// Project 从 Session 事件日志构造 Execution 读模型。
func Project(sess types.Session, events []event.Event) []types.Execution {
	executions := make([]types.Execution, 0)
	index := make(map[types.ExecutionID]int)
	for _, ev := range events {
		switch payload := ev.Payload.(type) {
		case event.ExecutionStarted:
			if ev.ExecutionID == "" {
				continue
			}
			exec := types.Execution{
				ID:              ev.ExecutionID,
				SessionID:       sess.ID,
				SessionPublicID: sess.PublicID,
				TenantID:        sess.TenantID,
				Kind:            payload.ExecutionKind,
				Status:          types.ExecutionStatusRunning,
				ProfileVersion:  payload.ProfileVersion,
				CreatedAt:       ev.CreatedAt,
			}
			if payload.Pipeline != nil {
				exec.Pipeline = &types.PipelineExecution{
					Name:         payload.Pipeline.Name,
					CapabilityID: payload.Pipeline.CapabilityID,
					Args:         json.RawMessage(payload.Pipeline.Args),
				}
			}
			index[ev.ExecutionID] = len(executions)
			executions = append(executions, exec)

		case event.ExecutionFinished:
			i, ok := index[ev.ExecutionID]
			if !ok || executions[i].Status != types.ExecutionStatusRunning {
				continue
			}
			executions[i].Status = payload.Status
			executions[i].Error = payload.Error
			finishedAt := ev.CreatedAt
			executions[i].FinishedAt = &finishedAt
			if payload.Pipeline != nil && executions[i].Pipeline != nil {
				executions[i].Pipeline.Output = json.RawMessage(payload.Pipeline.Output)
			}
		}
	}
	return executions
}
