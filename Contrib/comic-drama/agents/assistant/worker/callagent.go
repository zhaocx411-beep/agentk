// call_agent 能力：把 studio 里的子 agent（含漫剧）暴露成对话入口
// （assistant）可调用的工具，实现"路由到子 agent"而非"路由到工具"。
//
// 形态是 workflow capability + blocking Tool：router 的 agent loop 以
// child workflow 同步等待本能力完成（Temporal 里等待持久且免费），子 Agent
// 在自己的 AgentK Session 里完整跑一轮（含生图 / 视频等长任务），收口后
// 结果作为工具返回值回到 router 上下文。子 Session 通过返回的 sessionId
// 可以续用，同一件事的多轮修改共享子 Agent 的上下文。
package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"studio/platform/chat"
	"studio/platform/core"
)

const (
	// CapabilityID / WorkflowType / ToolName 是对外协议，改动会断掉在飞执行。
	// 注意：SDK v0.1.4 删除了 Tool.Name，但 staging 的 agent-k 核心仍要求
	// manifest 携带 tool name（缺了会被 announce 拒绝：`tool: name ""`），
	// 因此 SDK 固定在删除前的 commit（见 go.mod）。带 name 的旧格式清单
	// 新旧核心都接受；等 staging 全量升级后再升 v0.1.4 并删掉 Name。
	CapabilityID = "contrib.studio.call_agent"
	WorkflowType = "StudioCallAgent"
	ToolName     = "call_agent"
	ActivityName = "studio.call_agent.run"

	// 子 Agent 一轮可能包含视频生成等长任务，给足预算。
	runTimeout   = 40 * time.Minute
	pollInterval = 2 * time.Second
)

// ToolDescription 是 call_agent 注册为 LLM Tool 时的说明书。
const ToolDescription = "把任务转交给一个专项 Agent 执行并返回其回复与产物。" +
	"task 必须自包含：子 Agent 看不到当前对话，需把需求、素材 URL、参数完整转述。" +
	"返回的 sessionId 用于续聊：对同一件事的修改或追问请带上原 sessionId；" +
	"新话题则不带 sessionId 让其新开会话。" +
	"若返回 status=awaiting_user，把 questions 转述给用户，" +
	"再带同一 sessionId 把用户的回答作为 task 传回。"

type CallAgentInput struct {
	// Agent 目标专项 Agent 的 ID。
	Agent string `json:"agent"`
	// Task 转交给子 Agent 的完整任务描述（或对其提问的回答）。
	Task string `json:"task"`
	// SessionID 续用之前 call_agent 返回的子会话；空则新建。
	SessionID string `json:"sessionId,omitempty"`
}

type CallAgentQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// CallAgentArtifact 与账本 artifacts 元素同形（type/uri/description），
// core 会把结果里的 artifacts 数组折叠进 router 会话供前端渲染。
type CallAgentArtifact struct {
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Description string `json:"description,omitempty"`
}

type CallAgentOutput struct {
	// Status completed（本轮完成）或 awaiting_user（子 Agent 等用户回答）。
	Status string `json:"status"`
	// Reply 子 Agent 本轮回复全文。
	Reply string `json:"reply,omitempty"`
	// Questions status=awaiting_user 时子 Agent 提出的问题。
	Questions []CallAgentQuestion `json:"questions,omitempty"`
	// Artifacts 本轮产出的媒体资产。
	Artifacts []CallAgentArtifact `json:"artifacts,omitempty"`
	// SessionID 子会话 ID，续聊时原样传回。
	SessionID string `json:"sessionId"`
}

// CallAgentWorkflow 只是 activity 的薄壳：核心工作是 IO（HTTP 驱动 AgentK），
// 全部放在单个带心跳的 activity 里。
func CallAgentWorkflow(
	ctx workflow.Context,
	input CallAgentInput,
) (CallAgentOutput, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: runTimeout,
		HeartbeatTimeout:    2 * time.Minute,
		// 重试会向子会话重复投递消息，宁可失败让 router 决定怎么办。
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	}
	var output CallAgentOutput
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, options), ActivityName, input,
	).Get(ctx, &output)
	return output, err
}

// CallAgentActivities 持有 activity 依赖；PollInterval 供测试加速。
type CallAgentActivities struct {
	Client       core.AgentKClient
	Registry     chat.AgentRegistry
	PollInterval time.Duration
	Heartbeat    func(ctx context.Context)
}

func (a *CallAgentActivities) Run(
	ctx context.Context,
	input CallAgentInput,
) (CallAgentOutput, error) {
	definition, ok := a.Registry.SubAgent(input.Agent)
	if !ok {
		return CallAgentOutput{}, fmt.Errorf(
			"unknown agent %q（可用 ID 见 system prompt 列表）", input.Agent)
	}
	task := strings.TrimSpace(input.Task)
	if task == "" {
		return CallAgentOutput{}, fmt.Errorf("task must not be empty")
	}

	// 续用旧子会话：先把状态追平；追不上（会话丢失）就退回新建。
	sessionID := strings.TrimSpace(input.SessionID)
	state := chat.NewChatState()
	if sessionID != "" {
		if err := a.refresh(ctx, sessionID, &state); err != nil {
			sessionID = ""
			state = chat.NewChatState()
		}
	}
	if sessionID == "" {
		title := definition.Name + ": " + core.TruncateRunes(task, 24)
		created, err := a.Client.CreateSession(ctx, title, definition.Profile)
		if err != nil {
			return CallAgentOutput{}, fmt.Errorf("create sub-session: %w", err)
		}
		sessionID = created
	}

	baseline := state.LastEventID
	turnStart := len(state.Messages)
	textOffset := 0
	artifactOffset := 0
	if len(state.PendingAsks) > 0 {
		// 子 Agent 停在 ask_user 上：把 task 当作用户的回答闭合提问。
		// 恢复后的输出会继续追加在提问前的那条 assistant 消息上，本轮
		// 输出从该消息起算，并裁掉上一轮已经交付过的部分。
		if state.CurrentAssistant >= 0 && state.CurrentAssistant < len(state.Messages) {
			turnStart = state.CurrentAssistant
			resumed := &state.Messages[state.CurrentAssistant]
			textOffset = len(resumed.Text)
			artifactOffset = len(resumed.Artifacts)
		}
		ask := state.PendingAsks[0]
		answers := make([]core.ChatAnswer, 0, len(ask.Questions))
		for _, question := range ask.Questions {
			answers = append(answers, core.ChatAnswer{
				QuestionID: question.ID, Selected: []string{task},
			})
		}
		err := a.Client.AnswerQuestions(ctx, sessionID, ask.ToolCallID, answers)
		if err != nil {
			return CallAgentOutput{}, fmt.Errorf("answer sub-agent: %w", err)
		}
	} else if err := a.Client.PostMessage(ctx, sessionID, task); err != nil {
		return CallAgentOutput{}, fmt.Errorf("post to sub-session: %w", err)
	}

	return a.awaitTurn(ctx, sessionID, &state, turnCursor{
		baseline:       baseline,
		start:          turnStart,
		textOffset:     textOffset,
		artifactOffset: artifactOffset,
	})
}

// turnCursor 标记本轮输出的起点：从哪条消息开始、以及（续答时）该消息里
// 已经在上一轮交付过的文本 / 产物前缀长度。
type turnCursor struct {
	// baseline 投递前的账本折叠游标（事件 ID）：只有看到它之后的新事件
	// 才可能收口。
	baseline       string
	start          int
	textOffset     int
	artifactOffset int
}

func (a *CallAgentActivities) awaitTurn(
	ctx context.Context,
	sessionID string,
	state *chat.ChatState,
	cursor turnCursor,
) (CallAgentOutput, error) {
	interval := a.PollInterval
	if interval <= 0 {
		interval = pollInterval
	}
	for {
		if a.Heartbeat != nil {
			a.Heartbeat(ctx)
		}
		if err := a.refresh(ctx, sessionID, state); err != nil {
			return CallAgentOutput{}, fmt.Errorf("query sub-session: %w", err)
		}
		// 只有看到投递之后的新事件才可能收口，避免把投递前的静止状态
		// 误判成"本轮已完成"。
		if state.LastEventID > cursor.baseline {
			if state.ErrorMessage != "" {
				return CallAgentOutput{}, fmt.Errorf(
					"sub-agent %s failed: %s", sessionID, state.ErrorMessage)
			}
			if len(state.PendingAsks) > 0 {
				output := a.turnOutput(state, cursor, "awaiting_user", sessionID)
				for _, ask := range state.PendingAsks {
					for _, question := range ask.Questions {
						output.Questions = append(output.Questions, CallAgentQuestion{
							Question: question.Question,
							Options:  append([]string(nil), question.Options...),
						})
					}
				}
				return output, nil
			}
			// finishReason == "tool_calls" 是同步工具间隙的短暂 Idle，
			// 子 Agent 马上会开下一个 round，本轮尚未真正结束。
			if state.Status == chat.ChatIdle &&
				state.LastFinishReason != "tool_calls" {
				return a.turnOutput(state, cursor, "completed", sessionID), nil
			}
		}
		select {
		case <-ctx.Done():
			return CallAgentOutput{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (a *CallAgentActivities) turnOutput(
	state *chat.ChatState,
	cursor turnCursor,
	status string,
	sessionID string,
) CallAgentOutput {
	output := CallAgentOutput{Status: status, SessionID: sessionID}
	start := min(cursor.start, len(state.Messages))
	var parts []string
	for index := start; index < len(state.Messages); index++ {
		message := state.Messages[index]
		text := message.Text
		artifacts := message.Artifacts
		if index == cursor.start {
			if cursor.textOffset <= len(text) {
				text = text[cursor.textOffset:]
			}
			if cursor.artifactOffset <= len(artifacts) {
				artifacts = artifacts[cursor.artifactOffset:]
			}
		}
		if message.Role == "assistant" && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
		for _, artifact := range artifacts {
			output.Artifacts = append(output.Artifacts, CallAgentArtifact{
				Type:        artifact.Type,
				URI:         artifact.URI,
				Description: artifact.Description,
			})
		}
	}
	output.Reply = strings.Join(parts, "\n\n")
	return output
}

func (a *CallAgentActivities) refresh(
	ctx context.Context,
	sessionID string,
	state *chat.ChatState,
) error {
	events, err := a.Client.ListEvents(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, event := range events {
		state.Apply(event)
	}
	return nil
}
