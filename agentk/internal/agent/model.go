package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"agentk/internal/chat"
	"agentk/internal/event"
)

// Round 是一次模型调用生成的事件和工具调用。
type Round struct {
	// Drafts 按 RoundStarted、assistant 内容、tool call、RoundCompleted 的顺序排列，
	// 由调用方作为一个事件批原子提交。
	Drafts []event.Draft
	// Calls 本轮模型发起的工具调用（按模型给出的顺序）。
	Calls []chat.ToolCall
}

// RoundRef 标识模型调用及其输入事件位置。
type RoundRef struct {
	SessionPublicID string
	RoundID         event.RoundID
	// WorkerID 标识执行模型调用的 worker。
	WorkerID string
}

// CallModel 消费完整模型响应流并生成一个完整 round 的事件。
// 任一阶段失败都不返回部分 Draft；sink 仅接收非持久化增量，nil 表示禁用增量。
func CallModel(
	ctx context.Context,
	res Resolved,
	ref RoundRef,
	history []chat.Message,
	sink DeltaSink,
) (Round, error) {
	if res.Model == nil || res.Registry == nil {
		return Round{}, fmt.Errorf("agent: resolved model and registry are required")
	}
	roundID := ref.RoundID
	started := roundStarted(res, ref)
	start := time.Now()
	resp, err := res.Model.Stream(ctx, chat.Request{
		System:         res.System,
		Messages:       history,
		Tools:          toolDefs(res),
		IdempotencyKey: ModelInvocationKey(roundID),
	})
	if err != nil {
		return Round{}, fmt.Errorf("model call: %w", err)
	}
	for delta, err := range resp.Stream {
		if err != nil {
			return Round{}, fmt.Errorf("model stream: %w", err)
		}
		if sink != nil {
			sink.Delta(ref.SessionPublicID, roundID, delta)
		}
	}
	result, err := resp.Result()
	if err != nil {
		return Round{}, fmt.Errorf("model result: %w", err)
	}
	// 在生成事件前合并默认参数，使所有 binding 使用相同的最终参数。
	calls := make([]chat.ToolCall, len(result.ToolCalls))
	copy(calls, result.ToolCalls)
	for i := range calls {
		if spec, ok := res.Registry.Get(calls[i].Name); ok {
			calls[i].Args = spec.ApplyDefaults(calls[i].Args)
		}
	}

	drafts := make([]event.Draft, 0, len(result.Blocks)+len(calls)+2)
	drafts = append(drafts, event.Draft{RoundID: roundID, Payload: started})
	for i, block := range result.Blocks {
		switch block.Kind {
		case chat.BlockText:
			drafts = append(drafts, event.Draft{RoundID: roundID,
				Payload: event.AssistantText{Block: event.BlockIndex(i), Text: block.Text}})
		case chat.BlockReasoning:
			drafts = append(drafts, event.Draft{RoundID: roundID,
				Payload: event.AssistantReasoning{Block: event.BlockIndex(i), Text: block.Text}})
		}
	}
	for _, call := range calls {
		drafts = append(drafts, event.Draft{RoundID: roundID, Payload: event.AssistantToolCall{
			ToolCallID: event.ToolCallID(call.ID),
			ToolName:   call.Name,
			Args:       event.RawJSON(call.Args),
		}})
	}
	drafts = append(drafts, event.Draft{RoundID: roundID, Payload: event.RoundCompleted{
		Usage: event.TokenUsage{
			InputTokens:     result.Usage.InputTokens,
			OutputTokens:    result.Usage.OutputTokens,
			CacheReadTokens: result.Usage.CacheReadTokens,
		},
		ModelMillis:  time.Since(start).Milliseconds(),
		FinishReason: result.FinishReason,
	}})
	return Round{Drafts: drafts, Calls: calls}, nil
}

// roundStarted 生成模型调用的配置快照。Profile、模型、Dialect 和 prompt 摘要用于
// 追踪不同配置的结果；模型和 Dialect 也决定后续 fold 是否回传 reasoning。
func roundStarted(res Resolved, ref RoundRef) event.RoundStarted {
	sum := sha256.Sum256([]byte(res.System))
	specs := res.Registry.Specs()
	tools := make([]string, 0, len(specs))
	for _, s := range specs {
		tools = append(tools, s.Name)
	}
	return event.RoundStarted{
		Model:          res.ModelName,
		Dialect:        res.Dialect,
		ProfileVersion: res.Version,
		// 只持久化 prompt 摘要，避免每轮重复存储完整内容。
		PromptSHA256: hex.EncodeToString(sum[:])[:12],
		Tools:        tools,
		WorkerID:     ref.WorkerID,
	}
}

func toolDefs(res Resolved) []chat.ToolDef {
	specs := res.Registry.Specs()
	defs := make([]chat.ToolDef, 0, len(specs))
	for _, s := range specs {
		defs = append(defs, chat.ToolDef{
			Name:        s.Name,
			Description: s.Description,
			InputSchema: s.InputSchema,
		})
	}
	return defs
}
