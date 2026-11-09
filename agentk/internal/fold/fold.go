// Package fold 将 Session 事件投影为模型对话历史和查询视图。
// 投影是每次调用时重新计算的纯函数，不物化第二份状态；修改规则无需迁移历史数据。
// 历史按 round 分组，保证同一模型轮次的 assistant tool call 与结果相邻。
package fold

import (
	"encoding/json"
	"log"
	"slices"
	"strings"

	"agentk/internal/chat"
	"agentk/internal/event"
	"agentk/internal/tool"
)

// Options 配置对话历史投影。
type Options struct {
	// Registry 提供工具的 HistoryTrim 配置；为 nil 时不截断结果。
	Registry *tool.Registry
	// Model 与 Dialect 用于仅保留同一模型和协议生成的 reasoning；跨 provider 回传
	// 既无法保证语义兼容，也会污染 prompt cache。
	Model   string
	Dialect event.Dialect
}

// History 投影对话历史。events 必须按存储顺序传入。
func History(events []event.Event, opts Options) []chat.Message {
	p := newProjector(events, opts)
	for _, g := range groupByRound(events) {
		p.projectGroup(g)
	}
	return p.out
}

// group 包含一个 round 的全部事件，或一个 round 外的单独事件。
type group struct {
	round  event.RoundID
	events []event.Event
}

// groupByRound 按首次出现位置合并相同 round 的事件。
func groupByRound(events []event.Event) []group {
	var out []group
	at := make(map[event.RoundID]int)
	for _, ev := range events {
		if ev.RoundID == "" {
			out = append(out, group{events: []event.Event{ev}})
			continue
		}
		if i, known := at[ev.RoundID]; known {
			out[i].events = append(out[i].events, ev)
			continue
		}
		at[ev.RoundID] = len(out)
		out = append(out, group{round: ev.RoundID, events: []event.Event{ev}})
	}
	return out
}

type projector struct {
	opts Options
	out  []chat.Message
	// closed 记录已有成功、失败或用户回答结果的 tool call。
	closed map[event.ToolCallID]bool
	// toolNames 用于查找 HistoryTrim 配置和生成缺失结果。
	toolNames map[event.ToolCallID]string
}

// newProjector 预先索引所有 tool call 及其结果，因为结果可能在较后的事件中出现，
// 例如 ask_user 跨一次 Workflow 运行后才得到回答。
func newProjector(events []event.Event, opts Options) *projector {
	p := &projector{
		opts:      opts,
		closed:    make(map[event.ToolCallID]bool),
		toolNames: make(map[event.ToolCallID]string),
	}
	for _, ev := range events {
		switch payload := ev.Payload.(type) {
		case event.AssistantToolCall:
			p.toolNames[payload.ToolCallID] = payload.ToolName
		case event.ToolExecSucceeded:
			p.closed[payload.ToolCallID] = true
		case event.ToolExecFailed:
			p.closed[payload.ToolCallID] = true
		case event.AskUserResponded:
			p.closed[payload.ToolCallID] = true
		}
	}
	return p
}

// projectGroup 依次投影 assistant、tool/user 消息，并为缺失结果的 tool call 生成结果。
func (p *projector) projectGroup(g group) {
	asst := assistantDraft{}
	var trailing []chat.Message
	for _, ev := range g.events {
		switch payload := ev.Payload.(type) {
		case event.RoundStarted:
			asst.model, asst.dialect = payload.Model, payload.Dialect
		case event.AssistantText:
			asst.put(payload.Block, chat.BlockText, payload.Text)
		case event.AssistantReasoning:
			asst.put(payload.Block, chat.BlockReasoning, payload.Text)
		case event.AssistantToolCall:
			asst.calls = append(asst.calls, chat.ToolCall{
				ID:   string(payload.ToolCallID),
				Name: payload.ToolName,
				Args: json.RawMessage(payload.Args),
			})
		case event.UserInput:
			trailing = append(trailing, chat.Message{Role: chat.RoleUser, Text: payload.Text})
		case event.ToolExecSucceeded:
			trailing = append(trailing, p.toolResult(payload.ToolCallID, json.RawMessage(payload.Result)))
		case event.ToolExecFailed:
			trailing = append(trailing, p.toolError(payload.ToolCallID, payload.Error))
		case event.AskUserResponded:
			trailing = append(trailing, p.answerResult(payload))
		case event.RoundCompleted, event.ToolExecStarted, event.AskUserRequested,
			event.ExecutionStarted, event.ExecutionFinished:
			// trace、执行状态和控制流事件不产生新的模型消息。
		case event.InvocationProgress:
			// Pipeline 事件仅供客户端消费，不进入模型历史。
		}
	}
	if msg, ok := asst.build(p.opts); ok {
		p.out = append(p.out, msg)
	}
	p.out = append(p.out, trailing...)
	for _, call := range asst.calls {
		if !p.closed[event.ToolCallID(call.ID)] {
			p.out = append(p.out, p.unanswered(call))
		}
	}
}

// toolResult 投影工具结果，并应用该工具的 HistoryTrim 配置。
func (p *projector) toolResult(id event.ToolCallID, result json.RawMessage) chat.Message {
	return chat.Message{
		Role:       chat.RoleTool,
		ToolCallID: string(id),
		Text:       string(p.trim(id, result)),
	}
}

// toolError 将工具失败投影为对应 tool call 的结果；provider 要求每个调用都有结果。
func (p *projector) toolError(id event.ToolCallID, msg string) chat.Message {
	return chat.Message{
		Role:       chat.RoleTool,
		ToolCallID: string(id),
		Text:       jsonText(map[string]string{"error": msg}),
	}
}

// answerResult 将用户回答投影为 ask_user 的 tool result。
func (p *projector) answerResult(payload event.AskUserResponded) chat.Message {
	return chat.Message{
		Role:       chat.RoleTool,
		ToolCallID: string(payload.ToolCallID),
		Text:       jsonText(map[string]any{"answers": payload.Answers}),
	}
}

// trim 按工具声明的 HistoryTrim 确定性截断结果字段。同一事件每次得到相同投影，
// 避免基于时间的变化破坏历史前缀和 prompt cache。
func (p *projector) trim(id event.ToolCallID, result json.RawMessage) json.RawMessage {
	if p.opts.Registry == nil {
		return result
	}
	name := p.toolNames[id]
	spec, ok := p.opts.Registry.Get(name)
	if !ok || spec.HistoryTrim == nil {
		return result
	}
	trimmed, changed := trimField(result, *spec.HistoryTrim, name)
	if !changed {
		return result
	}
	return trimmed
}

// assistantDraft 收集一个 round 的 assistant 输出。
type assistantDraft struct {
	model   string
	dialect event.Dialect
	blocks  []draftBlock
	calls   []chat.ToolCall
}

type draftBlock struct {
	index event.BlockIndex
	kind  chat.BlockKind
	text  string
}

// put 按 block index 和 kind 替换重复内容。
func (d *assistantDraft) put(index event.BlockIndex, kind chat.BlockKind, text string) {
	for i, b := range d.blocks {
		if b.index == index && b.kind == kind {
			d.blocks[i].text = text
			return
		}
	}
	d.blocks = append(d.blocks, draftBlock{index: index, kind: kind, text: text})
}

// build 组装 assistant 消息；没有模型输出时返回 false。
func (d *assistantDraft) build(opts Options) (chat.Message, bool) {
	if len(d.blocks) == 0 && len(d.calls) == 0 {
		return chat.Message{}, false
	}
	ordered := slices.Clone(d.blocks)
	slices.SortStableFunc(ordered, func(a, b draftBlock) int { return int(a.index) - int(b.index) })
	var text, reasoning []string
	for _, b := range ordered {
		switch b.kind {
		case chat.BlockText:
			text = append(text, b.text)
		case chat.BlockReasoning:
			reasoning = append(reasoning, b.text)
		}
	}
	msg := chat.Message{
		Role:      chat.RoleAssistant,
		Text:      strings.Join(text, "\n\n"),
		ToolCalls: d.calls,
	}
	if d.model == opts.Model && d.dialect == opts.Dialect {
		msg.Reasoning = strings.Join(reasoning, "\n\n")
	}
	return msg, true
}

// unanswered 为没有持久化结果的 tool call 生成仅存在于内存投影的结果。
// 用户跳过 ask_user 或运行中断都可能留下悬空调用；若不补齐，provider 会拒绝整段
// 后续历史。事件中不记录该结果，因为它并未真实发生。
func (p *projector) unanswered(call chat.ToolCall) chat.Message {
	out := map[string]string{
		"error": "tool call did not complete (run interrupted); result unavailable",
	}
	if p.userSuppliedResult(call.Name) {
		// UserBinding 使用未回答状态，避免将用户跳过问题解释为工具故障。
		out = map[string]string{
			"status": "unanswered",
			"note":   "用户没有回答这次提问，而是直接说了别的；按他新说的来，不要重复提问。",
		}
	}
	return chat.Message{Role: chat.RoleTool, ToolCallID: call.ID, Text: jsonText(out)}
}

// userSuppliedResult 判断工具是否由用户提供结果；未知工具按 ask_user 名称兼容旧事件。
func (p *projector) userSuppliedResult(name string) bool {
	if p.opts.Registry != nil {
		if spec, ok := p.opts.Registry.Get(name); ok {
			_, isUser := spec.Binding.(tool.UserBinding)
			return isUser
		}
	}
	return name == tool.AskUserToolName
}

// jsonText 编码内部生成的 tool result；编码失败时返回稳定的错误结果。
func jsonText(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		log.Printf("fold: marshal %T: %v", v, err)
		return `{"error":"marshal tool result failed"}`
	}
	return string(raw)
}
