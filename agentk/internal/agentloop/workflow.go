package agentloop

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"agentk/internal/agent"
	"agentk/internal/event"
	"agentk/internal/execution"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
	"agentk/internal/types"
)

var (
	roundActivityOptions = workflow.ActivityOptions{
		StartToCloseTimeout:    15 * time.Minute,
		ScheduleToCloseTimeout: 45 * time.Minute,
		RetryPolicy:            temporalx.ActivityRetryPolicy(3),
		// 等待 activity 响应取消，避免终态事件与仍在执行的事件追加竞争。
		WaitForCancellation: true,
	}
	toolActivityOptions = workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Minute,
		ScheduleToCloseTimeout: 90 * time.Minute,
		RetryPolicy:            temporalx.ActivityRetryPolicy(3),
		WaitForCancellation:    true,
	}
	persistenceActivityOptions = workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		// 生命周期事件对可重试错误无限重试；不可重试 ApplicationError 仍立即返回。
		RetryPolicy: temporalx.ActivityRetryPolicy(0),
	}
)

// AgentWorkflow 处理一次 Agent Loop Execution，直到无需模型响应或需要等待用户回答。
func AgentWorkflow(
	ctx workflow.Context,
	input AgentWorkflowInput,
) (output AgentWorkflowOutput, workflowErr error) {
	ref := input.Execution
	defer func() {
		status := types.ExecutionStatusCompleted
		detail := ""
		if workflowErr != nil {
			detail = boundedDetail(workflowErr.Error())
			status = types.ExecutionStatusFailed
			if temporal.IsCanceledError(workflowErr) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
				status = types.ExecutionStatusCancelled
			}
		}
		if err := finishExecution(ctx, FinishExecutionInput{
			Execution: ref, Status: status, Error: detail,
		}); err != nil && workflowErr == nil {
			output = AgentWorkflowOutput{}
			workflowErr = err
		}
	}()

	beginCtx, _ := workflow.NewDisconnectedContext(ctx)
	beginCtx = workflow.WithActivityOptions(beginCtx, persistenceActivityOptions)
	if err := workflow.ExecuteActivity(beginCtx, BeginExecutionActivityName, BeginExecutionInput{
		Execution: ref, Trigger: input.Trigger,
	}).Get(beginCtx, nil); err != nil {
		return AgentWorkflowOutput{}, err
	}

	rounds := 0
	toolRounds := 0

	for {
		roundID := execution.RoundID(ref.ID, rounds+1)
		roundCtx := workflow.WithActivityOptions(ctx, roundActivityOptions)
		var round RoundOutput
		if err := workflow.ExecuteActivity(
			roundCtx, RoundActivityName, RoundInput{Execution: ref, RoundID: roundID},
		).Get(roundCtx, &round); err != nil {
			return AgentWorkflowOutput{}, normalizeWorkflowError(ctx, err)
		}
		maxToolRounds := agent.EffectiveMaxToolRounds(round.MaxToolRounds)

		if round.Idle {
			return AgentWorkflowOutput{Rounds: rounds}, nil
		}
		rounds++

		if len(round.ToolCalls) == 0 {
			return AgentWorkflowOutput{Rounds: rounds}, nil
		}

		awaitingUser, err := executeToolCalls(ctx, ref, roundID, round.ToolCalls)
		if err != nil {
			return AgentWorkflowOutput{}, normalizeWorkflowError(ctx, err)
		}
		if awaitingUser {
			// 提问已持久化；用户回答时会启动新的 AgentWorkflow。
			return AgentWorkflowOutput{Rounds: rounds}, nil
		}

		toolRounds++
		if toolRounds >= maxToolRounds {
			err := temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("session: exceeded max tool rounds %d", maxToolRounds),
				"MaxToolRoundsExceeded", nil)
			return AgentWorkflowOutput{}, err
		}
	}
}

func finishExecution(ctx workflow.Context, input FinishExecutionInput) error {
	disconnected, _ := workflow.NewDisconnectedContext(ctx)
	finishCtx := workflow.WithActivityOptions(disconnected, persistenceActivityOptions)
	return workflow.ExecuteActivity(
		finishCtx, FinishExecutionActivityName, input,
	).Get(finishCtx, nil)
}

// deferredMustBeAlone 说明 UserBinding 不能与立即返回结果的工具处于同一轮。
// 用户回答会延迟到达，而同轮其他工具的结果会立即入账；混用会把 tool call 与其
// result 拆开，生成 provider 不接受的历史。因此必须先完成提问，再在下一轮调用其他工具。
const deferredMustBeAlone = "ask_user 必须单独调用：先问清楚，拿到答复后再在下一轮里做事"

// executeToolCalls 并发执行一轮中的工具调用，等待所有调用后按输入顺序返回首个错误。
// 工具结果由 activity 持久化，不经 Workflow History 返回。
// 即使某个调用已经失败，也必须等待所有 future 完成，避免中断或收尾事件与仍在执行的
// 工具写入竞争。
func executeToolCalls(
	ctx workflow.Context,
	ref types.ExecutionRef,
	roundID event.RoundID,
	calls []ToolCallRef,
) (bool, error) {
	toolCtx := workflow.WithActivityOptions(ctx, toolActivityOptions)
	futures := make([]workflow.Future, len(calls))
	declaredErrs := make([]error, len(calls))
	var pending workflow.WaitGroup
	for i, call := range calls {
		switch {
		case call.Binding == tool.BindUser:
			futures[i] = workflow.ExecuteActivity(toolCtx, AskUserActivityName, AskUserInput{
				Execution: ref, RoundID: roundID, CallIdx: i, Call: call,
				MustBeAlone: len(calls) > 1,
			})
		case call.Binding == tool.BindActivity && call.Activity != nil:
			// Begin、派发和 Finish 在独立 workflow coroutine 中顺序执行。
			if pending == nil {
				pending = workflow.NewWaitGroup(ctx)
			}
			pending.Add(1)
			idx, c := i, call
			workflow.Go(ctx, func(ctx workflow.Context) {
				defer pending.Done()
				declaredErrs[idx] = runDeclaredTool(ctx, ref, roundID, idx, c)
			})
		case call.Binding == tool.BindWorkflow && call.Workflow != nil:
			if pending == nil {
				pending = workflow.NewWaitGroup(ctx)
			}
			pending.Add(1)
			idx, c := i, call
			workflow.Go(ctx, func(ctx workflow.Context) {
				defer pending.Done()
				declaredErrs[idx] = runWorkflowTool(ctx, ref, roundID, idx, c)
			})
		default:
			futures[i] = workflow.ExecuteActivity(toolCtx, ExecuteInlineToolActivityName,
				ToolCallActivityInput{Execution: ref, RoundID: roundID, CallIdx: i, Call: call})
		}
	}
	if pending != nil {
		pending.Wait(ctx)
	}
	var (
		firstErr     error
		awaitingUser bool
	)
	for i, future := range futures {
		if future == nil {
			if declaredErrs[i] != nil && firstErr == nil {
				firstErr = declaredErrs[i]
			}
			continue
		}
		switch calls[i].Binding {
		case tool.BindUser:
			var out AskUserOutput
			if err := future.Get(toolCtx, &out); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			awaitingUser = awaitingUser || out.AwaitingUser
		case tool.BindInline, tool.BindActivity, tool.BindWorkflow:
			var out ExecuteInlineToolOutput
			if err := future.Get(toolCtx, &out); err != nil && firstErr == nil {
				firstErr = err
			}
		default:
			// 未知 binding 兼容已写入 Workflow History 的旧数据。
			var out ExecuteInlineToolOutput
			if err := future.Get(toolCtx, &out); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return false, firstErr
	}
	return awaitingUser, nil
}

// runDeclaredTool 依次记录开始、派发 ActivityBinding 并持久化结果。
// 派发失败记录为 ToolExecFailed；记账失败或取消返回给 AgentWorkflow。
func runDeclaredTool(
	ctx workflow.Context,
	ref types.ExecutionRef,
	roundID event.RoundID,
	idx int,
	call ToolCallRef,
) error {
	persistenceCtx := workflow.WithActivityOptions(ctx, persistenceActivityOptions)
	var begin BeginToolOutput
	if err := workflow.ExecuteActivity(persistenceCtx, BeginToolActivityName, ToolCallActivityInput{
		Execution: ref, RoundID: roundID, CallIdx: idx, Call: call,
	}).Get(persistenceCtx, &begin); err != nil {
		return err
	}
	start := workflow.Now(ctx)
	dispatchCtx := dedicatedToolContext(ctx, *call.Activity)
	var result json.RawMessage
	dispatchErr := workflow.ExecuteActivity(
		dispatchCtx, call.Activity.Name, begin.Args).Get(dispatchCtx, &result)
	finish := FinishToolInput{
		Execution: ref, RoundID: roundID, CallIdx: idx,
		Call:           call,
		DurationMillis: workflow.Now(ctx).Sub(start).Milliseconds(),
	}
	if dispatchErr != nil {
		if temporal.IsCanceledError(dispatchErr) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
			return dispatchErr
		}
		finish.Error = boundedDetail(dispatchErr.Error())
	} else {
		finish.Result = event.RawJSON(result)
	}
	if err := workflow.ExecuteActivity(
		persistenceCtx, FinishToolActivityName, finish).Get(persistenceCtx, nil); err != nil {
		return err
	}
	return nil
}

// runWorkflowTool 记录 WorkflowBinding 开始并等待 child workflow。
// child 失败记录为 ToolExecFailed；记账失败或取消返回给 AgentWorkflow。
func runWorkflowTool(
	ctx workflow.Context,
	ref types.ExecutionRef,
	roundID event.RoundID,
	idx int,
	call ToolCallRef,
) error {
	persistenceCtx := workflow.WithActivityOptions(ctx, persistenceActivityOptions)
	var begin BeginToolOutput
	if err := workflow.ExecuteActivity(persistenceCtx, BeginToolActivityName, ToolCallActivityInput{
		Execution: ref, RoundID: roundID, CallIdx: idx, Call: call,
	}).Get(persistenceCtx, &begin); err != nil {
		return err
	}
	finish := FinishToolInput{
		Execution: ref, RoundID: roundID, CallIdx: idx,
		Call: call,
	}
	start := workflow.Now(ctx)
	childOpts := workflow.ChildWorkflowOptions{TaskQueue: call.Workflow.TaskQueue}
	if call.Workflow.ExecutionTimeoutSeconds > 0 {
		childOpts.WorkflowExecutionTimeout =
			time.Duration(call.Workflow.ExecutionTimeoutSeconds) * time.Second
	}
	childCtx := workflow.WithChildOptions(ctx, childOpts)
	var result json.RawMessage
	dispatchErr := workflow.ExecuteChildWorkflow(
		childCtx, call.Workflow.Type, begin.Args).Get(childCtx, &result)
	finish.DurationMillis = workflow.Now(ctx).Sub(start).Milliseconds()
	if dispatchErr != nil {
		if temporal.IsCanceledError(dispatchErr) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
			return dispatchErr
		}
		finish.Error = boundedDetail(dispatchErr.Error())
	} else {
		finish.Result = event.RawJSON(result)
	}
	if err := workflow.ExecuteActivity(
		persistenceCtx, FinishToolActivityName, finish).Get(persistenceCtx, nil); err != nil {
		return err
	}
	return nil
}

// dedicatedToolContext 将非零派发配置覆盖到默认 activity 配置。
// ScheduleToCloseTimeout 始终保留默认上限。
func dedicatedToolContext(ctx workflow.Context, d ActivityDispatch) workflow.Context {
	opts := toolActivityOptions
	opts.TaskQueue = d.TaskQueue
	if d.StartToCloseSeconds > 0 {
		opts.StartToCloseTimeout = time.Duration(d.StartToCloseSeconds) * time.Second
	}
	if d.MaxAttempts > 0 {
		opts.RetryPolicy = temporalx.ActivityRetryPolicy(int32(d.MaxAttempts))
	}
	return workflow.WithActivityOptions(ctx, opts)
}

func normalizeWorkflowError(ctx workflow.Context, err error) error {
	if temporal.IsCanceledError(err) || errors.Is(ctx.Err(), workflow.ErrCanceled) {
		return temporal.NewCanceledError("agent loop execution cancelled")
	}
	return err
}

const maxExecutionErrorDetail = 2000

func boundedDetail(detail string) string {
	if len(detail) <= maxExecutionErrorDetail {
		return detail
	}
	return detail[:maxExecutionErrorDetail] + "...(truncated)"
}
