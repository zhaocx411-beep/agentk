package event

import "agentk/internal/types"

// UserInput 是启动一次 Agent Loop 的用户消息。
type UserInput struct {
	Text string `json:"text"`
}

// Dialect 标识模型 API 的消息格式、reasoning 回传和 usage 语义。
type Dialect string

const DialectOpenAI Dialect = "openai"

// RoundStarted 记录一次模型调用使用的配置，用于追踪不同模型、Profile 和 prompt 的结果。
type RoundStarted struct {
	Model   string  `json:"model"`
	Dialect Dialect `json:"dialect"`
	// ProfileVersion 是本轮使用的 Profile 版本。
	ProfileVersion string `json:"profileVersion,omitempty"`
	// PromptSHA256 是 system prompt 的摘要。
	PromptSHA256 string `json:"promptSha256,omitempty"`
	// Tools 是本轮提供给模型的工具名。
	Tools []string `json:"tools,omitempty"`
	// WorkerID 标识执行本轮的 worker。
	WorkerID string `json:"workerId,omitempty"`
}

// TokenUsage 是一次模型调用的用量。
type TokenUsage struct {
	InputTokens     int `json:"inputTokens"`
	OutputTokens    int `json:"outputTokens"`
	CacheReadTokens int `json:"cacheReadTokens,omitempty"`
}

// RoundCompleted 记录一次模型调用的用量、耗时和结束原因。
type RoundCompleted struct {
	Usage TokenUsage `json:"usage"`
	// ModelMillis 是模型调用的耗时。
	ModelMillis int64 `json:"modelMillis"`
	// FinishReason 是 provider 返回的原始结束原因。
	FinishReason string `json:"finishReason,omitempty"`
}

// AssistantText 是模型产出的一段正文。
type AssistantText struct {
	Block BlockIndex `json:"block"`
	Text  string     `json:"text"`
}

// AssistantReasoning 保存模型返回的原始 reasoning，只投影给相同模型和 Dialect。
type AssistantReasoning struct {
	Block BlockIndex `json:"block"`
	Text  string     `json:"text"`
}

// AssistantToolCall 是模型发起的工具调用；Args 由具体工具解释。
type AssistantToolCall struct {
	ToolCallID ToolCallID `json:"toolCallId"`
	ToolName   string     `json:"toolName"`
	Args       RawJSON    `json:"args"`
}

// ToolExecStarted 标记工具开始执行。工具名和参数由 AssistantToolCall 提供。
type ToolExecStarted struct {
	ToolCallID ToolCallID `json:"toolCallId"`
}

// Artifact 描述工具产出的资源，不维护独立资源登记表。URI 是跨工具使用的资源句柄；
// 来源关系由对应 tool call 参数表达，Params 保存 provider 实际采用的生成参数。
type Artifact struct {
	// Type 是资源类型，例如 image 或 video。
	Type string `json:"type"`
	URI  string `json:"uri"`
	// Description 是工具提供的资源说明。
	Description string `json:"description,omitempty"`
	// Model 是工具执行时绑定的模型。
	Model string `json:"model,omitempty"`
	// Params 是工具实际使用的生成参数，可能包含输入中没有的 provider 默认值。
	Params RawJSON `json:"params,omitempty"`
}

// ToolExecSucceeded 是一次工具执行的成功结果。
type ToolExecSucceeded struct {
	ToolCallID ToolCallID `json:"toolCallId"`
	// Result 是工具返回的原始结果。
	Result RawJSON `json:"result"`
	// Artifacts 是本次执行产生的资源。
	Artifacts      []Artifact `json:"artifacts,omitempty"`
	DurationMillis int64      `json:"durationMillis"`
}

// ToolExecFailed 记录工具执行失败；fold 将其投影为对应调用的错误结果。
// 每个 tool call 都必须被结果或错误闭合，工具失败本身不终止 Session。
type ToolExecFailed struct {
	ToolCallID     ToolCallID `json:"toolCallId"`
	Error          string     `json:"error"`
	DurationMillis int64      `json:"durationMillis"`
}

// Question 是一道待用户回答的选择题。
type Question struct {
	// ID 是题目标识；缺失时由调用方按 q1..qn 生成。
	ID string `json:"id"`
	// Label 是可选的界面标题。
	Label    string `json:"label,omitempty"`
	Question string `json:"question"`
	// Options 是候选项；用户也可以提交自定义文本。
	Options []string `json:"options,omitempty"`
	// MultiSelect 表示是否允许多选。
	MultiSelect bool `json:"multiSelect,omitempty"`
}

// AskUserRequested 记录模型提问。AgentWorkflow 随后结束，等待期间没有活跃 Workflow；
// 回答事件写入后再启动新的运行。
type AskUserRequested struct {
	ToolCallID ToolCallID `json:"toolCallId"`
	Questions  []Question `json:"questions"`
}

// Answer 记录一道题的回答。Question 保留原文，使投影出的工具结果无需回查提问参数。
type Answer struct {
	QuestionID string `json:"questionId"`
	Question   string `json:"question"`
	// Selected 是用户选择或输入的文本。
	Selected []string `json:"selected"`
}

// AskUserResponded 是用户的作答，同时闭合那次 ask_user 调用。
type AskUserResponded struct {
	ToolCallID ToolCallID `json:"toolCallId"`
	Answers    []Answer   `json:"answers"`
}

type ExecutionStarted struct {
	ExecutionKind  types.ExecutionKind `json:"kind"`
	ProfileVersion string              `json:"profileVersion,omitempty"`
	Pipeline       *PipelineStarted    `json:"pipeline,omitempty"`
}

type PipelineStarted struct {
	Name         string  `json:"name"`
	CapabilityID string  `json:"capabilityId"`
	Args         RawJSON `json:"args,omitempty"`
}

// InvocationProgress 记录 capability invocation 的进度。信封字段属于公共协议，
// Data 对 event 包不透明，由具体 capability 解释。
type InvocationProgress struct {
	InvocationID   string `json:"invocationId"`
	InvocationKind string `json:"invocationKind"`
	InvocationName string `json:"invocationName"`
	// StepID 是 invocation 内的步骤标识，也是幂等键的一部分。
	StepID string `json:"stepId"`
	// Message 是面向用户的进度说明。
	Message string `json:"message,omitempty"`
	// Data 是步骤产生的可选结构化数据。
	Data RawJSON `json:"data,omitempty"`
}

type ExecutionFinished struct {
	Status   types.ExecutionStatus `json:"status"`
	Error    string                `json:"error,omitempty"`
	Pipeline *PipelineFinished     `json:"pipeline,omitempty"`
}

type PipelineFinished struct {
	Output RawJSON `json:"output,omitempty"`
}

func (UserInput) Kind() Kind          { return KindUserInput }
func (RoundStarted) Kind() Kind       { return KindRoundStarted }
func (RoundCompleted) Kind() Kind     { return KindRoundCompleted }
func (AssistantText) Kind() Kind      { return KindAssistantText }
func (AssistantReasoning) Kind() Kind { return KindAssistantReasoning }
func (AssistantToolCall) Kind() Kind  { return KindAssistantToolCall }
func (ToolExecStarted) Kind() Kind    { return KindToolExecStarted }
func (ToolExecSucceeded) Kind() Kind  { return KindToolExecSucceeded }
func (ToolExecFailed) Kind() Kind     { return KindToolExecFailed }
func (AskUserRequested) Kind() Kind   { return KindAskUserRequested }
func (AskUserResponded) Kind() Kind   { return KindAskUserResponded }
func (ExecutionStarted) Kind() Kind   { return KindExecutionStarted }
func (InvocationProgress) Kind() Kind { return KindInvocationProgress }
func (ExecutionFinished) Kind() Kind  { return KindExecutionFinished }

func (UserInput) isPayload()          {}
func (RoundStarted) isPayload()       {}
func (RoundCompleted) isPayload()     {}
func (AssistantText) isPayload()      {}
func (AssistantReasoning) isPayload() {}
func (AssistantToolCall) isPayload()  {}
func (ToolExecStarted) isPayload()    {}
func (ToolExecSucceeded) isPayload()  {}
func (ToolExecFailed) isPayload()     {}
func (AskUserRequested) isPayload()   {}
func (AskUserResponded) isPayload()   {}
func (ExecutionStarted) isPayload()   {}
func (InvocationProgress) isPayload() {}
func (ExecutionFinished) isPayload()  {}
