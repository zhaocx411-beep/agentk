package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

// ToolEnv 是执行工具所需的 Session 状态和默认配置。
// Artifacts 由事件 fold 得出，不查询独立状态表。
type ToolEnv struct {
	SessionPublicID string
	// Artifacts 是 list_artifacts 使用的事件投影。
	Artifacts []tool.ArtifactInfo
	// Timeout 非正时使用 DefaultToolTimeout。
	Timeout time.Duration
}

// ToolOutcome 是一次工具执行产生的事件。
type ToolOutcome struct {
	// Drafts 包含成功或失败结果，并作为同一批追加。
	Drafts []event.Draft
}

// ExecuteTool 执行工具调用。业务错误和工具超时转换为 ToolExecFailed，以闭合调用并
// 允许模型在下一轮修正；只有父 context 取消直接返回 error。不响应 context 的工具
// 可能继续占用 goroutine，直到自身返回。
func ExecuteTool(
	ctx context.Context,
	res Resolved,
	roundID event.RoundID,
	call chat.ToolCall,
	env ToolEnv,
) (ToolOutcome, error) {
	if res.Registry == nil {
		return ToolOutcome{}, fmt.Errorf("agent: resolved registry is required")
	}
	callID := event.ToolCallID(call.ID)
	start := time.Now()
	spec, ok := res.Registry.Get(call.Name)
	if !ok {
		return failed(roundID, callID, fmt.Sprintf("unknown tool %q", call.Name), start), nil
	}

	tc := tool.Context{
		SessionID:      env.SessionPublicID,
		ToolCallID:     call.ID,
		IdempotencyKey: ToolInvocationKey(roundID, callID),
		ListArtifacts:  func() ([]tool.ArtifactInfo, error) { return env.Artifacts, nil },
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = env.Timeout
	}
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type toolReturn struct {
		out any
		err error
	}
	var ret toolReturn
	done := make(chan toolReturn, 1)
	go func() {
		out, err := spec.Run(tctx, spec.ApplyDefaults(call.Args), tc)
		done <- toolReturn{out, err}
	}()
	select {
	case <-tctx.Done():
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		return failed(roundID, callID, fmt.Sprintf("tool timed out after %s", timeout), start), nil
	case ret = <-done:
	}

	if ret.err != nil {
		if ctx.Err() != nil {
			return ToolOutcome{}, ctx.Err()
		}
		if errors.Is(ret.err, context.DeadlineExceeded) && tctx.Err() != nil {
			return failed(roundID, callID, fmt.Sprintf("tool timed out after %s", timeout), start), nil
		}
		return failed(roundID, callID, ret.err.Error(), start), nil
	}
	raw, err := json.Marshal(ret.out)
	if err != nil {
		return failed(roundID, callID, fmt.Sprintf("marshal tool output: %v", err), start), nil
	}
	return succeeded(roundID, callID, raw, start), nil
}

func failed(roundID event.RoundID, callID event.ToolCallID, msg string, start time.Time) ToolOutcome {
	return ToolOutcome{Drafts: []event.Draft{{RoundID: roundID, Payload: event.ToolExecFailed{
		ToolCallID:     callID,
		Error:          msg,
		DurationMillis: time.Since(start).Milliseconds(),
	}}}}
}

// succeeded 将工具结果和资源转换为 ToolExecSucceeded。
func succeeded(
	roundID event.RoundID,
	callID event.ToolCallID,
	result json.RawMessage,
	start time.Time,
) ToolOutcome {
	return ToolOutcome{Drafts: []event.Draft{{RoundID: roundID, Payload: event.ToolExecSucceeded{
		ToolCallID:     callID,
		Result:         event.RawJSON(result),
		Artifacts:      tool.ExtractResultArtifacts(result),
		DurationMillis: time.Since(start).Milliseconds(),
	}}}}
}
