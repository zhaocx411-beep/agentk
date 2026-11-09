// Agent 模式：会话事件账本 → 漫剧任务快照的折叠。
//
// Agent Run 没有 pipeline-run 那样的单个状态端点：账本是唯一真相源。
// 这里按事件序增量折叠出下游关心的最小事实——现在是否还在干活、
// 模型最近说了什么、成片在哪。判定规则与 agent-k 的收敛语义对齐：
// 模型某一轮只输出正文、没有发起任何工具调用，即视为本次任务收敛（idle）；
// 有未闭合的工具调用或未终态的异步 Job 则仍在推进（running）。
//
// Pipeline 模式的进度折叠见 fold.go；状态常量统一在 store.go。
package comicdrama

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"studio/platform/core"
)

// composeToolNames 是产出成片的合成工具名集合：旧版 agent-k 暴露
// concat_videos，新版收敛为唯一的 compose_video。两者的成功结果里
// 都带最终视频 URL，只是位置不同（见 filmURL）。
var composeToolNames = map[string]struct{}{
	"concat_videos": {},
	"compose_video": {},
}

// DramaMessage 会话往来的一条消息：用户指令或模型某一轮的完整正文。
type DramaMessage struct {
	Role    string // "user" / "assistant"
	Text    string
	EventID string
	At      time.Time
}

// DramaAsset 一条产物：类型、URL、产出工具、模型给的描述与时间。
// URI 全会话去重（list_artifacts 等重放工具不重复入账）。
type DramaAsset struct {
	Kind        string // image / video / audio
	URI         string
	Tool        string
	Description string
	EventID     string
	At          time.Time
}

// DramaEdge 一条生成血缘：From 资产作为输入参与生成了 To 资产。
// 由工具调用入参里的 URL 与该调用的产物 URL 配对得出。
type DramaEdge struct {
	From string
	To   string
}

// DramaState 是一个漫剧任务对账本的增量折叠现场。字段导出是为了
// 内存 store 的深拷贝与测试断言，不对外序列化。
type DramaState struct {
	// LastEventID 已折叠到的最后一个事件 ID（evt_ UUIDv7，字典序 == 时间序）。
	LastEventID string
	// Converged 最近一轮模型输出是否不含工具调用（收敛）。
	Converged bool
	// OpenCalls 已发起未闭合的工具调用；ToolNames 记录调用名供产物归因。
	OpenCalls map[string]struct{}
	ToolNames map[string]string
	// OpenJobs 已提交未终态的异步任务；JobCalls 记录 job → 发起它的
	// 工具调用，异步产物凭它归因到工具（视频生成都走 job）。
	OpenJobs map[string]struct{}
	JobCalls map[string]string
	// CallInputs 每次工具调用入参里出现的 URL（生成血缘的输入侧）。
	CallInputs map[string][]string
	// Edges 生成血缘边（时间序）；EdgeKeys 是去重集合。
	Edges    []DramaEdge
	EdgeKeys map[string]struct{}
	// roundText / roundCalls 当前轮的累计现场。
	RoundText  string
	RoundCalls int
	// AssistantSays 最近一个有正文的轮次的完整文本。
	AssistantSays string
	// Messages 完整的会话往来（用户指令 + 模型各轮正文），时间序。
	Messages []DramaMessage
	// Assets 全会话产物清单（URI 去重，时间序）；SeenAssets 是去重集合。
	Assets     []DramaAsset
	SeenAssets map[string]struct{}
	// Films 合成工具（concat_videos/compose_video）产出的成片 URL
	// （时间序，含各集与全剧合并）。
	Films []string
	// ImageCount / VideoCount 全会话产物计数（成片也计入 VideoCount）。
	ImageCount int
	VideoCount int
	// ErrorMessage 最近一次执行失败、中断的说明；新的用户输入清空它。
	ErrorMessage string
}

func NewDramaState() DramaState {
	return DramaState{
		Converged:  true,
		OpenCalls:  make(map[string]struct{}),
		ToolNames:  make(map[string]string),
		OpenJobs:   make(map[string]struct{}),
		JobCalls:   make(map[string]string),
		CallInputs: make(map[string][]string),
		SeenAssets: make(map[string]struct{}),
		EdgeKeys:   make(map[string]struct{}),
	}
}

func (s *DramaState) Status() DramaStatus {
	// 在途工具调用/异步任务优先于 error：agent loop 被打断（如超过单次
	// 推进的 20 轮工具上限）时，独立的 JobWorkflow 仍在运行，任务完成后
	// 会按 wakePolicy=resume 自动唤醒会话继续推进——对外仍是 running。
	if len(s.OpenCalls) > 0 || len(s.OpenJobs) > 0 {
		return DramaRunning
	}
	if s.ErrorMessage != "" {
		return DramaError
	}
	if !s.Converged {
		return DramaRunning
	}
	return DramaIdle
}

// Apply 按序折叠一条账本事件。未知 kind 一律忽略：账本协议只增不改，
// 下游不该因为 agent-k 新增事件种类而坏掉。Connect 适配层每次返回全量
// 正序日志，已折叠过的前缀靠事件 ID 这道闸跳掉。
func (s *DramaState) Apply(ev core.LedgerEvent) {
	if ev.ID <= s.LastEventID {
		return
	}
	s.LastEventID = ev.ID
	switch ev.Kind {
	case "user.input":
		// 新指令接管会话：上一段的失败不再代表当前状态。
		s.ErrorMessage = ""
		s.Converged = false
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && strings.TrimSpace(data.Text) != "" {
			s.Messages = append(s.Messages, DramaMessage{
				Role: "user", Text: data.Text, EventID: ev.ID, At: ev.CreatedAt,
			})
		}

	case "round.started":
		// 新一轮开始即代表推进已恢复（用户消息或 job 完成唤醒了会话）：
		// 上一段的中断提示不再代表当前状态。
		s.ErrorMessage = ""
		s.Converged = false
		s.RoundText = ""
		s.RoundCalls = 0

	case "assistant.text":
		var data struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			s.RoundText += data.Text
		}

	case "assistant.tool_call":
		var data struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.ToolCallID != "" {
			s.OpenCalls[data.ToolCallID] = struct{}{}
			s.ToolNames[data.ToolCallID] = data.ToolName
			if inputs := extractURLs(data.Args); len(inputs) > 0 {
				s.CallInputs[data.ToolCallID] = inputs
			}
			s.RoundCalls++
		}

	case "round.completed":
		if strings.TrimSpace(s.RoundText) != "" {
			s.AssistantSays = s.RoundText
			s.Messages = append(s.Messages, DramaMessage{
				Role: "assistant", Text: s.RoundText, EventID: ev.ID, At: ev.CreatedAt,
			})
		}
		s.Converged = s.RoundCalls == 0

	case "tool.exec_succeeded":
		var data struct {
			ToolCallID string          `json:"toolCallId"`
			Result     json.RawMessage `json:"result"`
			Artifacts  []artifactData  `json:"artifacts"`
		}
		if json.Unmarshal(ev.Data, &data) != nil {
			return
		}
		delete(s.OpenCalls, data.ToolCallID)
		s.collectAssets(data.ToolCallID, data.Artifacts, ev)
		if _, ok := composeToolNames[s.ToolNames[data.ToolCallID]]; ok {
			if url := filmURL(data.Result, data.Artifacts); url != "" {
				s.Films = append(s.Films, url)
			}
		}

	case "tool.exec_failed":
		var data struct {
			ToolCallID string `json:"toolCallId"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			delete(s.OpenCalls, data.ToolCallID)
		}

	case "job.submitted":
		var data struct {
			JobID      string `json:"jobId"`
			ToolCallID string `json:"toolCallId"`
		}
		if json.Unmarshal(ev.Data, &data) == nil && data.JobID != "" {
			s.OpenJobs[data.JobID] = struct{}{}
			if data.ToolCallID != "" {
				s.JobCalls[data.JobID] = data.ToolCallID
			}
		}

	case "job.completed":
		var data struct {
			JobID      string         `json:"jobId"`
			ToolCallID string         `json:"toolCallId"`
			WakePolicy string         `json:"wakePolicy"`
			Artifacts  []artifactData `json:"artifacts"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			delete(s.OpenJobs, data.JobID)
			callID := data.ToolCallID
			if callID == "" {
				callID = s.JobCalls[data.JobID]
			}
			s.collectAssets(callID, data.Artifacts, ev)
			// resume 策略的任务完成后 agent-k 会唤醒会话开新一轮：
			// 即刻视为仍在推进，避免 error/idle 的短暂闪烁。
			if data.WakePolicy == "resume" {
				s.ErrorMessage = ""
				s.Converged = false
			}
		}

	case "job.failed":
		// 单个远端任务失败不判死任务：Agent 会被唤醒并按 profile 里的
		// 规则重试当前镜头，最终结果看后续轮次。
		var data struct {
			JobID string `json:"jobId"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			delete(s.OpenJobs, data.JobID)
		}

	case "execution.finished":
		// 新版 AgentK 的统一执行终态。completed 的正常收敛仍由
		// round.completed 判定；这里只处理会让旧 UI 卡住的失败和取消。
		var data struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(ev.Data, &data) == nil &&
			(data.Status == "failed" || data.Status == "cancelled") {
			s.ErrorMessage = strings.TrimSpace(data.Error)
			if s.ErrorMessage == "" {
				s.ErrorMessage = "execution " + data.Status
			}
			s.OpenCalls = make(map[string]struct{})
			s.OpenJobs = make(map[string]struct{})
			s.Converged = false
		}

	case "interrupted":
		var data struct {
			Reason string `json:"reason"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			message := data.Detail
			if message == "" {
				message = data.Reason
			}
			s.ErrorMessage = message
		}
		// agent loop 已死：随 loop 执行的同步工具调用不会再有闭合事件，
		// 清掉避免状态永远卡在 running。独立运行的异步 Job 保留——
		// 它们完成后会唤醒会话继续。
		s.OpenCalls = make(map[string]struct{})

	case "error":
		var data struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(ev.Data, &data) == nil {
			s.ErrorMessage = data.Message
		}
	}
}

type artifactData struct {
	Type        string `json:"type"`
	URI         string `json:"uri"`
	Description string `json:"description"`
}

// extractURLs 递归收集 JSON 值里所有 http(s) 字符串：工具入参的引用图
// （定妆照、场景图、首帧等）都以 URL 形式出现，这是血缘的输入侧。
func extractURLs(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var urls []string
	var walk func(v any)
	walk = func(v any) {
		if len(urls) >= 64 {
			return
		}
		switch typed := v.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
				urls = append(urls, trimmed)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			// 按键名有序遍历：map 随机迭代序会让血缘边顺序在两次折叠间
			// 漂移，同一账本必须折出同一状态。
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(typed[key])
			}
		}
	}
	walk(value)
	return urls
}

// filmURL 从合成工具的成功结果里提取成片 URL。旧契约把 URL 放在
// result.videoUrl；artifacts 标准化后的新契约只在 artifacts 里给出
// type=video 的 uri（事件顶层与 result 内各有一份，取哪份都行）。
func filmURL(result json.RawMessage, artifacts []artifactData) string {
	var r struct {
		VideoURL  string         `json:"videoUrl"`
		Artifacts []artifactData `json:"artifacts"`
	}
	if json.Unmarshal(result, &r) == nil {
		if url := strings.TrimSpace(r.VideoURL); url != "" {
			return url
		}
		artifacts = append(artifacts, r.Artifacts...)
	}
	for _, a := range artifacts {
		if a.Type == "video" && strings.TrimSpace(a.URI) != "" {
			return a.URI
		}
	}
	return ""
}

// collectAssets 把一次工具/任务成功携带的产物记入清单并更新计数，
// 同时把"调用入参里的已知资产 → 本次产物"配对成生成血缘边。
// URI 全会话去重：list_artifacts、job_status 这类只重放已有产物的
// 工具直接跳过，避免同一张图反复入账。
func (s *DramaState) collectAssets(callID string, artifacts []artifactData, ev core.LedgerEvent) {
	tool := s.ToolNames[callID]
	if tool == "list_artifacts" || tool == "job_status" {
		return
	}
	inputs := s.CallInputs[callID]
	for _, artifact := range artifacts {
		uri := strings.TrimSpace(artifact.URI)
		if uri == "" {
			continue
		}
		switch artifact.Type {
		case "image", "video", "audio":
		default:
			continue
		}
		if _, seen := s.SeenAssets[uri]; seen {
			continue
		}
		s.SeenAssets[uri] = struct{}{}
		s.Assets = append(s.Assets, DramaAsset{
			Kind: artifact.Type, URI: uri, Tool: tool,
			Description: strings.TrimSpace(artifact.Description),
			EventID:     ev.ID, At: ev.CreatedAt,
		})
		switch artifact.Type {
		case "image":
			s.ImageCount++
		case "video":
			s.VideoCount++
		}
		// 血缘：只连已登记的资产（用户上传等外部 URL 不进画布）。
		for _, input := range inputs {
			if input == uri {
				continue
			}
			if _, known := s.SeenAssets[input]; !known {
				continue
			}
			key := input + "\x00" + uri
			if _, dup := s.EdgeKeys[key]; dup {
				continue
			}
			s.EdgeKeys[key] = struct{}{}
			s.Edges = append(s.Edges, DramaEdge{From: input, To: uri})
		}
	}
}

// Clone 深拷贝折叠现场，供内存 store 隔离读写。
func (s DramaState) Clone() DramaState {
	clone := s
	clone.OpenCalls = make(map[string]struct{}, len(s.OpenCalls))
	for id := range s.OpenCalls {
		clone.OpenCalls[id] = struct{}{}
	}
	clone.ToolNames = make(map[string]string, len(s.ToolNames))
	for id, name := range s.ToolNames {
		clone.ToolNames[id] = name
	}
	clone.OpenJobs = make(map[string]struct{}, len(s.OpenJobs))
	for id := range s.OpenJobs {
		clone.OpenJobs[id] = struct{}{}
	}
	clone.JobCalls = make(map[string]string, len(s.JobCalls))
	for id, callID := range s.JobCalls {
		clone.JobCalls[id] = callID
	}
	clone.CallInputs = make(map[string][]string, len(s.CallInputs))
	for id, inputs := range s.CallInputs {
		clone.CallInputs[id] = append([]string(nil), inputs...)
	}
	clone.SeenAssets = make(map[string]struct{}, len(s.SeenAssets))
	for uri := range s.SeenAssets {
		clone.SeenAssets[uri] = struct{}{}
	}
	clone.EdgeKeys = make(map[string]struct{}, len(s.EdgeKeys))
	for key := range s.EdgeKeys {
		clone.EdgeKeys[key] = struct{}{}
	}
	clone.Messages = append([]DramaMessage(nil), s.Messages...)
	clone.Assets = append([]DramaAsset(nil), s.Assets...)
	clone.Edges = append([]DramaEdge(nil), s.Edges...)
	clone.Films = append([]string(nil), s.Films...)
	return clone
}
