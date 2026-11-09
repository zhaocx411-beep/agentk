package chat

// 全量消息 Chat 接口（converse）：前端每次把完整对话记录（含最新一条用户
// 消息）发过来，真相源在调用方手里。服务端把记录对齐到一个 AgentK Session：
// 能续上就只投递最新一条（增量，最省 token、天然命中前缀缓存）；对不上
// （服务重启、映射丢失、历史被修改）就新建 Session 并把历史回灌重建，前端
// 无感。回灌文本由 SeedText 生成，格式字节级稳定，保证重建路径的提示词
// 前缀在多次重建之间尽可能共享，前缀缓存仍然可命中。
//
// 对前端而言协议无状态：只需持有 conversationId 与消息数组本身；服务端的
// conversation 索引是纯内存缓存，丢了会自动走重建路径。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"studio/platform/core"
)

const (
	// converseDefaultWait / converseMaxWait 是单次请求同步等待收口的预算。
	// 超时返回 status=running，前端原样重发同一请求继续等（幂等）。
	converseDefaultWait = 25 * time.Second
	converseMaxWait     = 110 * time.Second

	// seedHeader / seedFooter 是回灌历史的包裹标记。它们参与提示词前缀，
	// 一经发布不得改动（改动等于换协议版本，会打散所有已缓存前缀）。
	seedHeader = "<chat_history>"
	seedFooter = "</chat_history>"

	// capabilityHint 只加在投递给 AgentK 的当轮用户消息上，不写进前端
	// 持有的 messages。system prompt 保持字节稳定；选择能力不换 profile。
	capabilityHintPrefix = "[用户指定能力: "
	capabilityHintSuffix = "]\n\n"
)

// ConverseMessage 是前端全量记录里的一条消息。Content 逐字节原样对待：
// 服务端不 trim、不改写；前端也必须原样保存服务端返回的 reply。
type ConverseMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type converseRequest struct {
	ConversationID string            `json:"conversationId"`
	Messages       []ConverseMessage `json:"messages"`
	// Stream 为 true 时以 SSE 返回 AgentK 的实时文本增量和最终结果。
	// 也可以通过 Accept: text/event-stream 开启，便于客户端渐进迁移。
	Stream bool `json:"stream,omitempty"`
	// SelectedAgent 用户本轮指定的子 agent ID（如 comic-drama）。空则
	// 由模型按意图自动路由。只作为提示词标记，不换会话 profile。
	SelectedAgent string `json:"selectedAgent,omitempty"`
}

type converseResponse struct {
	ConversationID string `json:"conversationId"`
	// Status：completed（本轮收口）、awaiting_user（agent 反问，等用户回答）、
	// running（还在执行，原样重发本请求继续等）、error（本轮失败，可重发重试）。
	Status string `json:"status"`
	// Reply 是本轮助手回复全文。completed / awaiting_user 且非空时，前端必须
	// 把它按 {"role":"assistant","content":Reply} 原样追加进本地记录。
	Reply            string         `json:"reply,omitempty"`
	PendingQuestions []ChatQuestion `json:"pendingQuestions,omitempty"`
	Artifacts        []ChatArtifact `json:"artifacts,omitempty"`
	// Rebuilt 表示本次请求走了"新建 Session + 回灌历史"路径（仅供观测）。
	Rebuilt bool   `json:"rebuilt"`
	Error   string `json:"error,omitempty"`
}

// conversation 是服务端对一段对话的内存索引：AgentK session 绑定、折叠
// 状态与对齐哈希。整条记录丢失无害——下一次请求会重建回灌。
type conversation struct {
	mu        sync.Mutex
	sessionID string
	state     ChatState
	// acceptedHash 是最近一次收口后"双方共识的全量记录"哈希（含服务端
	// 返回的 reply）。新请求的 messages[:len-1] 必须命中它才允许增量投递。
	acceptedHash string
	// inFlightHash 是正在推进的请求全量哈希；相同请求重放即"继续等待"。
	inFlightHash string
	// turnStart 是本轮开始前 state.Messages 的长度，reply 从这里向后取。
	turnStart int
	// executionID 用于把当前消息与 AgentK WatchEvents 的服务端流绑定。
	executionID string
	// lastRequestHash / lastResponse 支持已收口请求的幂等重放。
	lastRequestHash string
	lastResponse    converseResponse
}

type conversationStore struct {
	mu    sync.Mutex
	items map[string]*conversation
}

func newConversationStore() *conversationStore {
	return &conversationStore{items: make(map[string]*conversation)}
}

func (s *conversationStore) acquire(key string) *conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	convo, ok := s.items[key]
	if !ok {
		convo = &conversation{state: NewChatState(), turnStart: -1}
		s.items[key] = convo
	}
	return convo
}

func (s *conversationStore) drop(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
}

// SeedText 把全量记录序列化成回灌 Session 的首条消息。
//
// 格式契约（字节级稳定，参与提示词前缀缓存，不得改动）：
//   - 只有一条消息时原样返回（全新对话不加包裹）；
//   - 否则为 seedHeader + "\n"，每条历史消息 "[role]\n" + content + "\n\n"，
//     seedFooter + "\n\n"，最后是最新一条用户消息原文。
//
// 历史条目 append-only：更长记录的序列化以更短记录的序列化为前缀
// （直到 footer 前），多次重建之间提示词前缀仍然共享。
func SeedText(messages []ConverseMessage) string {
	if len(messages) == 1 {
		return messages[0].Content
	}
	var builder strings.Builder
	builder.WriteString(seedHeader)
	builder.WriteByte('\n')
	for _, message := range messages[:len(messages)-1] {
		builder.WriteString("[")
		builder.WriteString(message.Role)
		builder.WriteString("]\n")
		builder.WriteString(message.Content)
		builder.WriteString("\n\n")
	}
	builder.WriteString(seedFooter)
	builder.WriteString("\n\n")
	builder.WriteString(messages[len(messages)-1].Content)
	return builder.String()
}

// annotateUserTurn 在投递给 AgentK 的当轮用户消息前加上能力标记。
// 前端 messages 仍是用户原文；标记只存在于 AgentK 账本这一侧。
func annotateUserTurn(content, selectedAgent string) string {
	selectedAgent = strings.TrimSpace(selectedAgent)
	if selectedAgent == "" {
		return content
	}
	return capabilityHintPrefix + selectedAgent + capabilityHintSuffix + content
}

func seedDelivery(messages []ConverseMessage, selectedAgent string) string {
	if strings.TrimSpace(selectedAgent) == "" {
		return SeedText(messages)
	}
	annotated := append([]ConverseMessage(nil), messages...)
	last := len(annotated) - 1
	annotated[last].Content = annotateUserTurn(annotated[last].Content, selectedAgent)
	return SeedText(annotated)
}

// hashConverseMessages 计算全量记录的对齐哈希（role 与 content 逐字节参与）。
func hashConverseMessages(messages []ConverseMessage) string {
	digest := sha256.New()
	for _, message := range messages {
		digest.Write([]byte(message.Role))
		digest.Write([]byte{0})
		digest.Write([]byte(message.Content))
		digest.Write([]byte{0, 1})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func hashConverseRequest(messages []ConverseMessage, selectedAgent string) string {
	digest := sha256.New()
	digest.Write([]byte(hashConverseMessages(messages)))
	digest.Write([]byte{0, 2})
	digest.Write([]byte(strings.TrimSpace(selectedAgent)))
	return hex.EncodeToString(digest.Sum(nil))
}

func (s *Server) handleConverse(w http.ResponseWriter, r *http.Request) {
	definition, ok := s.agents.Get(r.PathValue("agentID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var request converseRequest
	if err := core.DecodeOneJSON(r.Body, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if problem := validateConverseMessages(request.Messages); problem != "" {
		core.WriteError(w, http.StatusBadRequest, problem)
		return
	}
	streamRequested := request.Stream || acceptsEventStream(r)
	var streamClient core.AgentKStreamingClient
	if streamRequested {
		var streamOK bool
		streamClient, streamOK = s.agentk.(core.AgentKStreamingClient)
		if !streamOK {
			core.WriteError(w, http.StatusNotImplemented, "AgentK streaming is not available")
			return
		}
		if _, ok := w.(http.Flusher); !ok {
			core.WriteError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}
	}
	selectedAgent := strings.TrimSpace(request.SelectedAgent)
	if selectedAgent != "" {
		if _, ok := s.agents.SubAgent(selectedAgent); !ok {
			core.WriteError(w, http.StatusBadRequest, "unknown selectedAgent")
			return
		}
		// 能力标记只对统一对话入口有意义；专项 chat 页自己就是该能力。
		if definition.ID != "assistant" {
			selectedAgent = ""
		}
	}
	conversationID := strings.TrimSpace(request.ConversationID)
	if conversationID == "" {
		generated, err := newConversationID()
		if err != nil {
			core.WriteError(w, http.StatusInternalServerError, "failed to create conversation")
			return
		}
		conversationID = generated
	}

	key := definition.ID + "/" + conversationID
	convo := s.conversations.acquire(key)
	convo.mu.Lock()
	defer convo.mu.Unlock()

	fullHash := hashConverseRequest(request.Messages, selectedAgent)
	prefixHash := hashConverseMessages(request.Messages[:len(request.Messages)-1])
	latest := request.Messages[len(request.Messages)-1]
	delivery := annotateUserTurn(latest.Content, selectedAgent)
	rebuilt := false

	switch {
	case convo.sessionID != "" && convo.inFlightHash == fullHash:
		// 同一请求重放：本轮仍在推进，直接继续等待。
	case convo.sessionID != "" && convo.inFlightHash == "" &&
		convo.lastRequestHash == fullHash:
		// 已收口请求的幂等重放：原样返回上次的结果。
		if streamRequested {
			writeCachedConverseStream(w, convo.lastResponse)
		} else {
			core.WriteJSON(w, http.StatusOK, convo.lastResponse)
		}
		return
	case convo.sessionID != "" && convo.inFlightHash == "" &&
		convo.acceptedHash == prefixHash:
		// 前缀对齐：只投递最新一条，AgentK 账本里的历史继续生效。
		if err := s.refreshConversation(r.Context(), convo); err != nil {
			core.WriteError(w, http.StatusBadGateway, "failed to query AgentK")
			return
		}
		convo.turnStart = len(convo.state.Messages)
		executionID, err := s.startConverseMessage(r.Context(), convo.sessionID, delivery)
		if err != nil {
			log.Printf("converse: post message to %s: %v", convo.sessionID, err)
			core.WriteError(w, http.StatusBadGateway, "failed to send message")
			return
		}
		convo.executionID = executionID
		convo.inFlightHash = fullHash
	default:
		// 对不上（映射丢失 / 历史被改 / 全新对话）：新建 Session 回灌重建。
		rebuilt = convo.sessionID != "" || len(request.Messages) > 1
		title := core.TruncateRunes(firstUserContent(request.Messages), 28)
		sessionID, err := s.agentk.CreateSession(
			r.Context(), definition.Name+": "+title, definition.Profile,
		)
		if err != nil {
			log.Printf("converse: create session: %v", err)
			core.WriteError(w, http.StatusBadGateway, "failed to create AgentK session")
			return
		}
		executionID, err := s.startConverseMessage(
			r.Context(), sessionID, seedDelivery(request.Messages, selectedAgent),
		)
		if err != nil {
			log.Printf("converse: seed session %s: %v", sessionID, err)
			core.WriteError(w, http.StatusBadGateway, "failed to send message")
			return
		}
		convo.sessionID = sessionID
		convo.state = NewChatState()
		convo.turnStart = 0
		convo.executionID = executionID
		convo.acceptedHash = ""
		convo.lastRequestHash = ""
		convo.inFlightHash = fullHash
	}

	if streamRequested {
		response, terminal := s.streamConverseTurn(
			w, r, streamClient, convo, conversationID, rebuilt,
		)
		if terminal {
			s.finishConverseTurn(key, convo, request.Messages, fullHash, response)
		}
		return
	}

	response := s.awaitConverseTurn(r, convo, conversationID)
	response.Rebuilt = response.Rebuilt || rebuilt
	s.finishConverseTurn(key, convo, request.Messages, fullHash, response)
	core.WriteJSON(w, http.StatusOK, response)
}

func acceptsEventStream(r *http.Request) bool {
	for _, value := range strings.Split(r.Header.Get("Accept"), ",") {
		mediaType := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
		if strings.EqualFold(mediaType, "text/event-stream") {
			return true
		}
	}
	return false
}

func (s *Server) startConverseMessage(
	ctx context.Context,
	sessionID string,
	text string,
) (string, error) {
	if streaming, ok := s.agentk.(core.AgentKStreamingClient); ok {
		return streaming.StartMessage(ctx, sessionID, text)
	}
	return "", s.agentk.PostMessage(ctx, sessionID, text)
}

func (s *Server) finishConverseTurn(
	key string,
	convo *conversation,
	messages []ConverseMessage,
	fullHash string,
	response converseResponse,
) {
	switch response.Status {
	case "completed", "awaiting_user":
		accepted := messages
		if response.Reply != "" {
			accepted = append(
				append([]ConverseMessage(nil), messages...),
				ConverseMessage{Role: "assistant", Content: response.Reply},
			)
		}
		convo.acceptedHash = hashConverseMessages(accepted)
		convo.inFlightHash = ""
		convo.executionID = ""
		convo.lastRequestHash = fullHash
		convo.lastResponse = response
	case "error":
		// 失败的轮次留在旧 Session 账本里没法撤销；丢掉映射，下次同一
		// 请求走重建回灌，干净重试。
		s.conversations.drop(key)
	}
}

type converseStreamMeta struct {
	ConversationID string `json:"conversationId"`
	Status         string `json:"status"`
	Rebuilt        bool   `json:"rebuilt"`
}

type converseStreamDelta struct {
	RoundID    string `json:"roundId,omitempty"`
	Generation string `json:"generation"`
	Index      int32  `json:"index"`
	Block      int32  `json:"block"`
	Text       string `json:"text"`
}

type converseStreamError struct {
	Error string `json:"error"`
}

type converseSSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newConverseSSEWriter(w http.ResponseWriter) *converseSSEWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	return &converseSSEWriter{w: w, flusher: w.(http.Flusher)}
}

func (w *converseSSEWriter) send(event string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

func (w *converseSSEWriter) heartbeat() error {
	if _, err := fmt.Fprint(w.w, ": keep-alive\n\n"); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

func writeCachedConverseStream(w http.ResponseWriter, response converseResponse) {
	stream := newConverseSSEWriter(w)
	if err := stream.send("meta", converseStreamMeta{
		ConversationID: response.ConversationID,
		Status:         response.Status,
		Rebuilt:        response.Rebuilt,
	}); err != nil {
		return
	}
	_ = stream.send("done", response)
}

// streamConverseTurn 转发当前 execution 的 AgentK 服务端流。只有 text delta
// 对外暴露，reasoning delta 不进入接口；最终 done 始终以持久化账本为准。
func (s *Server) streamConverseTurn(
	w http.ResponseWriter,
	r *http.Request,
	client core.AgentKStreamingClient,
	convo *conversation,
	conversationID string,
	rebuilt bool,
) (converseResponse, bool) {
	stream := newConverseSSEWriter(w)
	if err := stream.send("meta", converseStreamMeta{
		ConversationID: conversationID,
		Status:         "running",
		Rebuilt:        rebuilt,
	}); err != nil {
		return converseResponse{}, false
	}

	frames := make(chan core.AgentKStreamFrame)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- client.WatchEvents(
			r.Context(), convo.sessionID, convo.executionID,
			func(frame core.AgentKStreamFrame) error {
				select {
				case frames <- frame:
					return nil
				case <-r.Context().Done():
					return r.Context().Err()
				}
			},
		)
	}()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case frame := <-frames:
			if frame.Event != nil {
				convo.state.Apply(*frame.Event)
			}
			if delta := frame.Delta; delta != nil && delta.Kind == "text" && delta.Text != "" {
				if err := stream.send("delta", converseStreamDelta{
					RoundID:    delta.RoundID,
					Generation: delta.Generation,
					Index:      delta.Index,
					Block:      delta.Block,
					Text:       delta.Text,
				}); err != nil {
					return converseResponse{}, false
				}
			}
		case err := <-watchDone:
			if r.Context().Err() != nil {
				return converseResponse{}, false
			}
			if refreshErr := s.refreshConversation(r.Context(), convo); refreshErr != nil {
				log.Printf("converse: refresh after stream: %v", refreshErr)
				_ = stream.send("error", converseStreamError{Error: "failed to query AgentK"})
				return converseResponse{}, false
			}
			response := converseResponseFromState(convo, conversationID)
			response.Rebuilt = rebuilt
			if response.Status == "running" {
				if err != nil {
					log.Printf("converse: watch execution %s: %v", convo.executionID, err)
				}
				_ = stream.send("error", converseStreamError{Error: "AgentK stream ended before turn completed"})
				return response, false
			}
			if sendErr := stream.send("done", response); sendErr != nil {
				return converseResponse{}, false
			}
			return response, true
		case <-heartbeat.C:
			if err := stream.heartbeat(); err != nil {
				return converseResponse{}, false
			}
		case <-r.Context().Done():
			return converseResponse{}, false
		}
	}
}

// awaitConverseTurn 轮询账本直到本轮收口或等待预算耗尽。
func (s *Server) awaitConverseTurn(
	r *http.Request,
	convo *conversation,
	conversationID string,
) converseResponse {
	response := converseResponse{ConversationID: conversationID, Status: "running"}
	deadline := time.Now().Add(converseWaitBudget(r))
	for {
		if err := s.refreshConversation(r.Context(), convo); err != nil {
			response.Status = "error"
			response.Error = "failed to query AgentK"
			return response
		}
		response = converseResponseFromState(convo, conversationID)
		if response.Status != "running" {
			return response
		}
		if time.Now().After(deadline) {
			return response
		}
		select {
		case <-r.Context().Done():
			response.Status = "running"
			return response
		case <-time.After(s.pollInterval):
		}
	}
}

func converseResponseFromState(convo *conversation, conversationID string) converseResponse {
	response := converseResponse{ConversationID: conversationID, Status: "running"}
	state := &convo.state
	sawInput := false
	for _, message := range state.Messages[min(convo.turnStart, len(state.Messages)):] {
		if message.Role == "user" {
			sawInput = true
			break
		}
	}
	if !sawInput {
		return response
	}
	if state.ErrorMessage != "" {
		response.Status = "error"
		response.Error = state.ErrorMessage
		response.Reply = turnReply(state, convo.turnStart, nil)
		return response
	}
	if len(state.PendingAsks) > 0 {
		response.Status = "awaiting_user"
		response.PendingQuestions = pendingQuestions(state)
		response.Reply = turnReply(state, convo.turnStart, state.PendingAsks)
		response.Artifacts = turnArtifacts(state, convo.turnStart)
		return response
	}
	// finishReason == "tool_calls" 是同步工具间隙的短暂 Idle，本轮尚未真正结束。
	if state.Status == ChatIdle && state.LastFinishReason != "tool_calls" {
		response.Status = "completed"
		response.Reply = turnReply(state, convo.turnStart, nil)
		response.Artifacts = turnArtifacts(state, convo.turnStart)
		return response
	}
	response.Reply = turnReply(state, convo.turnStart, nil)
	return response
}

func (s *Server) refreshConversation(ctx context.Context, convo *conversation) error {
	events, err := s.agentk.ListEvents(ctx, convo.sessionID)
	if err != nil {
		return err
	}
	for _, event := range events {
		convo.state.Apply(event)
	}
	return nil
}

// turnReply 拼接本轮全部助手文本。规则参与协议契约（前端会把结果原样追加
// 为 assistant 消息，服务端再据此校验前缀），必须保持确定性：
//   - 取 turnStart 之后所有 assistant 消息的 Text，跳过空白，用 "\n\n" 连接；
//   - agent 反问（ask_user）时，把问题与选项按固定格式附在末尾，保证回灌
//     重建后模型仍能看到自己问过什么。
func turnReply(state *ChatState, turnStart int, asks []ChatPendingAsk) string {
	if turnStart < 0 || turnStart > len(state.Messages) {
		turnStart = 0
	}
	parts := make([]string, 0, 4)
	for _, message := range state.Messages[turnStart:] {
		if message.Role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(message.Text); text != "" {
			parts = append(parts, message.Text)
		}
	}
	for _, ask := range asks {
		for _, question := range ask.Questions {
			var builder strings.Builder
			builder.WriteString(question.Question)
			if len(question.Options) > 0 {
				builder.WriteString("\n可选项：")
				builder.WriteString(strings.Join(question.Options, " / "))
			}
			parts = append(parts, builder.String())
		}
	}
	return strings.Join(parts, "\n\n")
}

func turnArtifacts(state *ChatState, turnStart int) []ChatArtifact {
	if turnStart < 0 || turnStart > len(state.Messages) {
		turnStart = 0
	}
	var artifacts []ChatArtifact
	for _, message := range state.Messages[turnStart:] {
		artifacts = append(artifacts, message.Artifacts...)
	}
	return artifacts
}

func pendingQuestions(state *ChatState) []ChatQuestion {
	var questions []ChatQuestion
	for _, ask := range state.PendingAsks {
		questions = append(questions, ask.Questions...)
	}
	return questions
}

// validateConverseMessages 返回问题描述；合法时返回空串。
func validateConverseMessages(messages []ConverseMessage) string {
	if len(messages) == 0 {
		return "messages is required"
	}
	for index, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			return fmt.Sprintf("messages[%d].role must be user or assistant", index)
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Sprintf("messages[%d].content must not be empty", index)
		}
	}
	if messages[len(messages)-1].Role != "user" {
		return "last message must have role user"
	}
	return ""
}

func firstUserContent(messages []ConverseMessage) string {
	for _, message := range messages {
		if message.Role == "user" {
			return message.Content
		}
	}
	return messages[0].Content
}

func converseWaitBudget(r *http.Request) time.Duration {
	raw := strings.TrimSpace(r.URL.Query().Get("wait"))
	if raw == "" {
		return converseDefaultWait
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return converseDefaultWait
	}
	budget := time.Duration(seconds) * time.Second
	if budget > converseMaxWait {
		return converseMaxWait
	}
	return budget
}

func newConversationID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "cnv_" + hex.EncodeToString(raw), nil
}
