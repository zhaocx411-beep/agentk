package agentloop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"agentk/internal/agent"
	"agentk/internal/chat"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/execution"
	"agentk/internal/fold"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
	"agentk/internal/types"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/heartbeat"
)

// maxInlineToolArgs 限制写入 Workflow History 的工具参数大小；超限参数只保存在
// AssistantToolCall 事件中，由执行 Activity 定点回载。
const maxInlineToolArgs = 128 << 10

// Store 是 agentloop 对存储的依赖。
type Store interface {
	GetSession(ctx context.Context, id int64) (types.Session, error)
	AppendEvents(ctx context.Context, sessionID int64, executionID types.ExecutionID, batchID event.BatchID, drafts []event.Draft) ([]event.Event, error)
	LoadSessionEvents(ctx context.Context, sessionID int64) ([]event.Event, error)
}

// Config 是 Activities 的依赖。
type Config struct {
	Store   Store
	Runtime agent.Runtime
	// WorkerID 写入 RoundStarted。
	WorkerID string
	// ToolTimeout 是默认工具超时，可由工具配置覆盖。
	ToolTimeout time.Duration
	// Deltas 为 nil 时不保存模型流式增量；完整结果仍通过 Session 事件提供。
	Deltas deltabuf.Buffer
}

// Activities 持有 worker 内共享的运行时依赖。
type Activities struct {
	store       Store
	runtime     agent.Runtime
	workerID    string
	toolTimeout time.Duration
	deltas      deltabuf.Buffer
}

func NewActivities(cfg Config) *Activities {
	return &Activities{
		store:       cfg.Store,
		runtime:     cfg.Runtime,
		workerID:    cfg.WorkerID,
		toolTimeout: cfg.ToolTimeout,
		deltas:      cfg.Deltas,
	}
}

// BeginExecutionActivity 初始化实时缓冲，再幂等写入 Execution 起点和外部输入。
func (a *Activities) BeginExecutionActivity(ctx context.Context, input BeginExecutionInput) error {
	sess, err := a.loadSession(ctx, input.Execution.Session)
	if err != nil {
		return temporalx.ClassifyActivityError("begin execution", err)
	}
	if input.Execution.ID == "" {
		return temporalx.ClassifyActivityError("begin execution",
			temporalx.InvalidActivityInput("executionId is required"))
	}
	draft, err := input.Trigger.Draft()
	if err != nil {
		return temporalx.ClassifyActivityError("begin execution",
			temporalx.InvalidActivityInput("trigger: %v", err))
	}
	if a.deltas != nil {
		if err := a.deltas.Begin(ctx, input.Execution.ID); err != nil {
			log.Printf("agentloop: begin execution stream %s: %v", input.Execution.ID, err)
		}
	}
	drafts := []event.Draft{{Payload: event.ExecutionStarted{
		ExecutionKind:  types.ExecutionKindAgentLoop,
		ProfileVersion: execution.ProfileVersion(sess.Profile),
	}}, draft}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		execution.BatchID(input.Execution.ID, "started"), drafts); err != nil {
		return temporalx.ClassifyActivityError("begin execution: append events", err)
	}
	return nil
}

func (a *Activities) FinishExecutionActivity(
	ctx context.Context, input FinishExecutionInput,
) error {
	if input.Execution.ID == "" {
		return temporalx.ClassifyActivityError("finish execution",
			temporalx.InvalidActivityInput("executionId is required"))
	}
	if input.Status != types.ExecutionStatusCompleted &&
		input.Status != types.ExecutionStatusFailed &&
		input.Status != types.ExecutionStatusCancelled {
		return temporalx.ClassifyActivityError("finish execution",
			temporalx.InvalidActivityInput("invalid terminal status %q", input.Status))
	}
	sess, err := a.loadSession(ctx, input.Execution.Session)
	if err != nil {
		return temporalx.ClassifyActivityError("finish execution", err)
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		execution.BatchID(input.Execution.ID, "finished"), []event.Draft{{
			Payload: event.ExecutionFinished{Status: input.Status, Error: input.Error},
		}}); err != nil {
		return temporalx.ClassifyActivityError("finish execution: append event", err)
	}
	if a.deltas != nil {
		if err := a.deltas.End(ctx, input.Execution.ID); err != nil {
			log.Printf("agentloop: end execution stream %s: %v", input.Execution.ID, err)
		}
	}
	return nil
}

// RoundActivity 投影事件、调用模型并原子追加完整 round。
// 模型输出不可重复计算，必须以 round 为事务边界：RoundID 同时作为批幂等键，
// Activity 重试只能提交第一份完整结果，fold 不会看到半个 round。
func (a *Activities) RoundActivity(ctx context.Context, input RoundInput) (RoundOutput, error) {
	sess, res, err := a.resolveSession(ctx, input.Execution.Session)
	if err != nil {
		return RoundOutput{}, temporalx.ClassifyActivityError("round", err)
	}
	if input.RoundID == "" {
		return RoundOutput{}, temporalx.ClassifyActivityError("round",
			temporalx.InvalidActivityInput("roundId is required"))
	}
	events, err := a.store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		return RoundOutput{}, temporalx.ClassifyActivityError("round: load events", err)
	}
	opts := fold.Options{Registry: res.Registry, Model: res.ModelName, Dialect: res.Dialect}
	status := fold.LoopStatus(events, opts)
	if !status.NeedsRound {
		return RoundOutput{
			Idle:          true,
			MaxToolRounds: agent.EffectiveMaxToolRounds(res.MaxToolRounds),
		}, nil
	}

	stop := heartbeat.Start(ctx)
	defer stop()
	round, err := agent.CallModel(ctx, res, agent.RoundRef{
		SessionPublicID: sess.PublicID,
		RoundID:         input.RoundID,
		WorkerID:        a.workerID,
	}, fold.History(events, opts), a.deltaSink(ctx, input.Execution.ID))
	if err != nil {
		return RoundOutput{}, temporalx.ClassifyActivityError("round: call model", err)
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		event.BatchID(input.RoundID), round.Drafts); err != nil {
		return RoundOutput{}, temporalx.ClassifyActivityError("round: append events", err)
	}
	return RoundOutput{
		ToolCalls:     toolCallRefs(round.Calls, res.Registry),
		MaxToolRounds: agent.EffectiveMaxToolRounds(res.MaxToolRounds),
	}, nil
}

// toolCallRefs 决定参数是否内联，并将当前 Profile Registry 中的 Binding 固定到
// Workflow History，使后续路由不受目录或 Profile 变化影响。
func toolCallRefs(calls []chat.ToolCall, reg *tool.Registry) []ToolCallRef {
	refs := make([]ToolCallRef, 0, len(calls))
	for _, call := range calls {
		ref := ToolCallRef{ToolCallID: event.ToolCallID(call.ID), ToolName: call.Name}
		if spec, known := reg.Get(call.Name); known {
			ref.Binding = spec.BindingKind()
			switch b := spec.Binding.(type) {
			case tool.ActivityBinding:
				ref.DefaultArgs = append(json.RawMessage(nil), spec.DefaultArgs...)
				ref.Activity = &ActivityDispatch{
					Name:                b.ActivityName,
					TaskQueue:           b.TaskQueue,
					StartToCloseSeconds: int64(b.StartToCloseTimeout / time.Second),
					MaxAttempts:         b.MaxAttempts,
				}
			case tool.WorkflowBinding:
				ref.DefaultArgs = append(json.RawMessage(nil), spec.DefaultArgs...)
				ref.Workflow = &WorkflowDispatch{
					Type:                    b.WorkflowType,
					TaskQueue:               b.TaskQueue,
					ExecutionTimeoutSeconds: int64(b.ExecutionTimeout / time.Second),
				}
			}
		}
		if len(call.Args) > maxInlineToolArgs {
			ref.ArgsInEventLog = true
		} else {
			ref.Args = call.Args
		}
		refs = append(refs, ref)
	}
	return refs
}

// ExecuteInlineToolActivity 持久化开始事件、执行 InlineBinding 并持久化结果。
// 结果不写入 Workflow History；下一轮从 Session 事件重新 fold，避免重复保存大结果。
func (a *Activities) ExecuteInlineToolActivity(
	ctx context.Context,
	input ToolCallActivityInput,
) (ExecuteInlineToolOutput, error) {
	return a.runToolCall(ctx, input)
}

// BeginToolActivity 是 ActivityBinding 和 WorkflowBinding 的记账前半：回载完整调用、
// 追加 ToolExecStarted，并返回按调用快照合并默认值后的参数。执行体由 Workflow 按
// 已固定的 capability ABI 直接派发。
func (a *Activities) BeginToolActivity(
	ctx context.Context,
	input ToolCallActivityInput,
) (BeginToolOutput, error) {
	sess, err := a.loadSession(ctx, input.Execution.Session)
	if err != nil {
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool", err)
	}
	call, err := a.resolveCall(ctx, sess.ID, input.RoundID, input.Call)
	if err != nil {
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool", err)
	}
	switch input.Call.Binding {
	case tool.BindActivity:
		if input.Call.Activity == nil {
			return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool",
				temporalx.InvalidActivityInput("tool %q is missing activity dispatch", call.Name))
		}
	case tool.BindWorkflow:
		if input.Call.Workflow == nil {
			return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool",
				temporalx.InvalidActivityInput("tool %q is missing workflow dispatch", call.Name))
		}
	case tool.BindInline, tool.BindUser:
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool",
			temporalx.InvalidActivityInput("tool %q is not workflow-dispatched", call.Name))
	default:
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool",
			temporalx.InvalidActivityInput("tool %q is not workflow-dispatched", call.Name))
	}
	frozenSpec := tool.Spec{DefaultArgs: input.Call.DefaultArgs}
	args, err := decodeToolArgs(frozenSpec.ApplyDefaults(call.Args))
	if err != nil {
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool",
			temporalx.InvalidActivityInput("%v", err))
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		batchKey(input.RoundID, "s", input.CallIdx),
		[]event.Draft{{
			RoundID: input.RoundID,
			Payload: event.ToolExecStarted{ToolCallID: event.ToolCallID(call.ID)},
		}}); err != nil {
		return BeginToolOutput{}, temporalx.ClassifyActivityError("begin tool: append started", err)
	}
	return BeginToolOutput{Args: args}, nil
}

// decodeToolArgs 将参数转为 map，避免 Temporal data converter 二次编码 RawMessage。
func decodeToolArgs(args json.RawMessage) (map[string]any, error) {
	decoded := map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &decoded); err != nil {
			return nil, fmt.Errorf("tool args must be a JSON object: %w", err)
		}
		if decoded == nil {
			decoded = map[string]any{}
		}
	}
	return decoded, nil
}

// FinishToolActivity 是派发工具的记账后半：按 ToolCallRef 持久化结果或失败，不重新
// 读取 Registry，并从固定 result.artifacts envelope 提取资源。批键由 round 和调用
// 序号确定，使重试幂等。
func (a *Activities) FinishToolActivity(ctx context.Context, input FinishToolInput) (FinishToolOutput, error) {
	sess, err := a.loadSession(ctx, input.Execution.Session)
	if err != nil {
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool", err)
	}
	if input.Call.ToolCallID == "" || input.Call.ToolName == "" {
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool",
			temporalx.InvalidActivityInput("toolCallId and toolName are required"))
	}

	var drafts []event.Draft
	switch {
	case input.Error != "":
		drafts = []event.Draft{{RoundID: input.RoundID, Payload: event.ToolExecFailed{
			ToolCallID:     input.Call.ToolCallID,
			Error:          input.Error,
			DurationMillis: input.DurationMillis,
		}}}
	case input.Call.Binding != tool.BindActivity && input.Call.Binding != tool.BindWorkflow:
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool",
			temporalx.InvalidActivityInput(
				"tool %q is not workflow-dispatched", input.Call.ToolName))
	case input.Call.Binding == tool.BindActivity && input.Call.Activity == nil:
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool",
			temporalx.InvalidActivityInput(
				"tool %q is missing activity dispatch", input.Call.ToolName))
	case input.Call.Binding == tool.BindWorkflow && input.Call.Workflow == nil:
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool",
			temporalx.InvalidActivityInput(
				"tool %q is missing workflow dispatch", input.Call.ToolName))
	default:
		result := input.Result
		if len(result) == 0 {
			result = event.RawJSON("null")
		}
		drafts = []event.Draft{{RoundID: input.RoundID, Payload: event.ToolExecSucceeded{
			ToolCallID:     input.Call.ToolCallID,
			Result:         result,
			Artifacts:      tool.ExtractResultArtifacts(json.RawMessage(result)),
			DurationMillis: input.DurationMillis,
		}}}
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		batchKey(input.RoundID, "t", input.CallIdx),
		drafts); err != nil {
		return FinishToolOutput{}, temporalx.ClassifyActivityError("finish tool: append result", err)
	}
	return FinishToolOutput{}, nil
}

// runToolCall 是 InlineBinding 的事件记录边界：claim-check 回载调用，幂等追加开始
// 事件，执行后再幂等追加结果和资源。Temporal-dispatched Binding 使用 Begin/Finish。
func (a *Activities) runToolCall(
	ctx context.Context,
	input ToolCallActivityInput,
) (ExecuteInlineToolOutput, error) {
	sess, res, err := a.resolveSession(ctx, input.Execution.Session)
	if err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool", err)
	}
	call, err := a.resolveCall(ctx, sess.ID, input.RoundID, input.Call)
	if err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool", err)
	}
	events, err := a.store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool: load events", err)
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		batchKey(input.RoundID, "s", input.CallIdx),
		[]event.Draft{{
			RoundID: input.RoundID,
			Payload: event.ToolExecStarted{ToolCallID: event.ToolCallID(call.ID)},
		}}); err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool: append started", err)
	}

	stop := heartbeat.Start(ctx)
	defer stop()
	outcome, err := agent.ExecuteTool(ctx, res, input.RoundID, call, agent.ToolEnv{
		SessionPublicID: sess.PublicID,
		Artifacts:       fold.Artifacts(events),
		Timeout:         a.toolTimeout,
	})
	if err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool", err)
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
		batchKey(input.RoundID, "t", input.CallIdx),
		outcome.Drafts); err != nil {
		return ExecuteInlineToolOutput{}, temporalx.ClassifyActivityError("execute inline tool: append result", err)
	}
	return ExecuteInlineToolOutput{}, nil
}

// AskUserActivity 校验并持久化 UserBinding 提问，不产生 tool result；用户回答事件
// 才完成该次调用。它是编排控制流，不经过普通工具超时和 Registry 执行路径。
// 无效或混合调用记录为 ToolExecFailed，使模型可在下一轮修正，且不登记 interaction。
func (a *Activities) AskUserActivity(ctx context.Context, input AskUserInput) (AskUserOutput, error) {
	sess, err := a.loadSession(ctx, input.Execution.Session)
	if err != nil {
		return AskUserOutput{}, temporalx.ClassifyActivityError("ask user", err)
	}
	call, err := a.resolveCall(ctx, sess.ID, input.RoundID, input.Call)
	if err != nil {
		return AskUserOutput{}, temporalx.ClassifyActivityError("ask user", err)
	}
	batch := batchKey(input.RoundID, "a", input.CallIdx)
	callID := event.ToolCallID(call.ID)

	reject := func(msg string) (AskUserOutput, error) {
		draft := event.Draft{RoundID: input.RoundID, Payload: event.ToolExecFailed{
			ToolCallID: callID, Error: msg,
		}}
		if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess,
			batch, []event.Draft{draft}); err != nil {
			return AskUserOutput{}, temporalx.ClassifyActivityError("ask user: append rejection", err)
		}
		return AskUserOutput{}, nil
	}
	if input.MustBeAlone {
		return reject(deferredMustBeAlone)
	}
	questions, err := tool.ParseAskQuestions(call.Args)
	if err != nil {
		return reject(err.Error())
	}
	if err := a.appendExecutionEvents(ctx, input.Execution.ID, sess, batch, []event.Draft{{
		RoundID: input.RoundID,
		Payload: event.AskUserRequested{ToolCallID: callID, Questions: questions},
	}}); err != nil {
		return AskUserOutput{}, temporalx.ClassifyActivityError("ask user: append request", err)
	}
	return AskUserOutput{AwaitingUser: true}, nil
}

// batchKey 使用 round 内序号生成长度受控的事件批幂等键；模型生成的 tool call ID
// 长度不受 AgentK 控制，不适合作为批键组成部分。
func batchKey(roundID event.RoundID, tag string, idx int) event.BatchID {
	return event.BatchID(fmt.Sprintf("%s#%s%d", roundID, tag, idx))
}

// appendExecutionEvents 先原子追加事件，再将已提交事件写入 Execution buffer。
// Session 事件先成立，实时通知才可发送；Buffer 写入失败只损失实时性。
func (a *Activities) appendExecutionEvents(
	ctx context.Context,
	executionID types.ExecutionID,
	sess types.Session,
	batch event.BatchID,
	drafts []event.Draft,
) error {
	if len(drafts) == 0 {
		return nil
	}
	events, err := a.store.AppendEvents(ctx, sess.ID, executionID, batch, drafts)
	if err != nil {
		return err
	}
	if a.deltas != nil {
		for _, ev := range events {
			raw, err := json.Marshal(ev)
			if err != nil {
				log.Printf("agentloop: marshal event %s for execution stream: %v", ev.ID, err)
				continue
			}
			if err := a.deltas.Append(ctx, executionID, deltabuf.Item{
				Kind: string(ev.Payload.Kind()), EventID: ev.ID, Data: raw,
			}); err != nil {
				log.Printf("agentloop: append event %s to execution stream: %v", ev.ID, err)
			}
		}
	}
	return nil
}

// deltaSink 将模型增量写入当前 Execution。Activity 重试使用新的 Gen；
// 重连客户端可按 Gen 识别新尝试并替换旧的未提交预览。
func (a *Activities) deltaSink(
	ctx context.Context, executionID types.ExecutionID,
) agent.DeltaSink {
	if a.deltas == nil {
		return nil
	}
	return &deltaAppender{ctx: ctx, buf: a.deltas, executionID: executionID, gen: newGen()}
}

// deltaAppender 由单个模型响应流顺序调用。
type deltaAppender struct {
	ctx         context.Context
	buf         deltabuf.Buffer
	executionID types.ExecutionID
	gen         string
	idx         int
}

func (d *deltaAppender) Delta(_ string, roundID event.RoundID, delta chat.Delta) {
	raw, err := json.Marshal(deltabuf.Frame{
		RoundID: roundID,
		Gen:     d.gen,
		Idx:     d.idx,
		Block:   delta.Block,
		Kind:    string(delta.Kind),
		Text:    delta.Text,
	})
	if err != nil {
		log.Printf("agentloop: marshal delta for %s: %v", d.executionID, err)
		return
	}
	err = d.buf.Append(d.ctx, d.executionID, deltabuf.Item{
		Kind: deltabuf.DeltaKind, Data: raw,
	})
	if err != nil {
		log.Printf("agentloop: append delta for %s: %v", d.executionID, err)
		return // Idx 不前进：缓冲前缀不能有洞
	}
	d.idx++
}

// newGen 生成 activity 尝试级的短随机标识。
func newGen() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand 失败说明环境已不可用
	}
	return hex.EncodeToString(b[:])
}

func (a *Activities) loadSession(ctx context.Context, ref types.SessionRef) (types.Session, error) {
	if a.store == nil {
		return types.Session{}, temporalx.ConfigurationError(fmt.Errorf("event store is required"))
	}
	if ref.SessionID <= 0 || ref.SessionPublicID == "" {
		return types.Session{}, temporalx.InvalidActivityInput("sessionId and sessionPublicId are required")
	}
	sess, err := a.store.GetSession(ctx, ref.SessionID)
	if err != nil {
		return types.Session{}, err
	}
	if sess.PublicID != ref.SessionPublicID {
		return types.Session{}, temporalx.InvalidActivityInput("session id/public id mismatch")
	}
	return sess, nil
}

// resolveSession 载入 Session 并解析其固定 Profile。
func (a *Activities) resolveSession(
	ctx context.Context,
	ref types.SessionRef,
) (types.Session, agent.Resolved, error) {
	if a.runtime == nil {
		return types.Session{}, agent.Resolved{}, temporalx.ConfigurationError(fmt.Errorf("runtime is required"))
	}
	if a.workerID == "" {
		return types.Session{}, agent.Resolved{}, temporalx.ConfigurationError(fmt.Errorf("worker id is required"))
	}
	sess, err := a.loadSession(ctx, ref)
	if err != nil {
		return types.Session{}, agent.Resolved{}, err
	}
	if len(sess.Profile) == 0 {
		return types.Session{}, agent.Resolved{}, temporalx.ConfigurationError(
			fmt.Errorf("session %s has no pinned profile", sess.PublicID))
	}
	res, err := a.runtime.Resolve(ctx, sess.Profile)
	if err != nil {
		return types.Session{}, agent.Resolved{}, temporalx.ConfigurationError(
			fmt.Errorf("resolve pinned session profile: %w", err))
	}
	if res.Model == nil || res.Registry == nil {
		return types.Session{}, agent.Resolved{}, temporalx.ConfigurationError(
			fmt.Errorf("resolved model and registry are required"))
	}
	return sess, res, nil
}

// resolveCall 还原工具调用；未内联的参数从对应 AssistantToolCall 事件回载。
func (a *Activities) resolveCall(
	ctx context.Context,
	sessionID int64,
	roundID event.RoundID,
	ref ToolCallRef,
) (chat.ToolCall, error) {
	if ref.ToolCallID == "" || ref.ToolName == "" {
		return chat.ToolCall{}, temporalx.InvalidActivityInput("toolCallId and toolName are required")
	}
	call := chat.ToolCall{ID: string(ref.ToolCallID), Name: ref.ToolName, Args: ref.Args}
	if !ref.ArgsInEventLog {
		return call, nil
	}
	events, err := a.store.LoadSessionEvents(ctx, sessionID)
	if err != nil {
		return chat.ToolCall{}, err
	}
	for _, ev := range events {
		payload, ok := ev.Payload.(event.AssistantToolCall)
		if ok && ev.RoundID == roundID && payload.ToolCallID == ref.ToolCallID {
			call.Args = json.RawMessage(payload.Args)
			return call, nil
		}
	}
	return chat.ToolCall{}, temporalx.InvalidActivityInput(
		"tool call %s not found in round %s", ref.ToolCallID, roundID)
}
