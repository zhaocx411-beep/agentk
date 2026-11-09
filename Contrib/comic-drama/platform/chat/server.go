package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"studio/platform/core"
)

const chatPollInterval = 750 * time.Millisecond

// Server 通用 Chat 服务：一个 Chat 一对一绑定一个 AgentK Session，
// Profile 由注册表（各 Agent 目录的 profile.json）固定，前端无法覆盖。
type Server struct {
	agentk        core.AgentKClient
	chats         *MemoryChatStore
	conversations *conversationStore
	agents        AgentRegistry
	uploads       UploadStorage
	now           func() time.Time
	newChatID     func() (string, error)
	pollInterval  time.Duration
}

func NewServer(agentk core.AgentKClient, agents AgentRegistry, uploads UploadStorage) *Server {
	return &Server{
		agentk:        agentk,
		chats:         NewMemoryChatStore(),
		conversations: newConversationStore(),
		agents:        agents,
		uploads:       uploads,
		now:           time.Now,
		newChatID:     newChatID,
		pollInterval:  chatPollInterval,
	}
}

// Register 把聊天与上传路由挂到 mux 上。
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", s.handleListAgents)
	mux.HandleFunc("POST /v1/agents/{agentID}/chats", s.handleCreateChat)
	mux.HandleFunc("GET /v1/chats", s.handleListChats)
	mux.HandleFunc("GET /v1/chats/{chatID}", s.handleGetChat)
	mux.HandleFunc("POST /v1/chats/{chatID}/messages", s.handlePostChatMessage)
	mux.HandleFunc("POST /v1/chats/{chatID}/answers", s.handleAnswerChatQuestions)
	mux.HandleFunc("GET /v1/chats/{chatID}/events", s.handleListChatEvents)
	mux.HandleFunc("GET /v1/chats/{chatID}/stream", s.handleChatStream)
	mux.HandleFunc("POST /v1/uploads", s.handleUpload)
	// converse：前端持有全量记录的无状态 Chat 接口（见 converse.go 与
	// docs/converse-api.md）。
	mux.HandleFunc("POST /v1/agents/{agentID}/converse", s.handleConverse)
}

// Handler 返回只含聊天路由的 http.Handler（测试用）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

type createChatRequest struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

type answerChatRequest struct {
	ToolCallID string       `json:"toolCallId"`
	Answers    []ChatAnswer `json:"answers"`
}

type postMessageRequest struct {
	Text string `json:"text"`
}

func (s *Server) handleListAgents(w http.ResponseWriter, _ *http.Request) {
	core.WriteJSON(w, http.StatusOK, map[string]any{"agents": s.agents.PublicList()})
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	definition, ok := s.agents.Get(r.PathValue("agentID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "agent not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var request createChatRequest
	if err := core.DecodeOneJSON(r.Body, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	request.Title = strings.TrimSpace(request.Title)
	if request.Message == "" {
		core.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}
	if request.Title == "" {
		request.Title = core.TruncateRunes(request.Message, 28)
	}

	chatID, err := s.newChatID()
	if err != nil {
		log.Printf("chat api: generate id: %v", err)
		core.WriteError(w, http.StatusInternalServerError, "failed to create chat")
		return
	}
	sessionID, err := s.agentk.CreateSession(
		r.Context(),
		definition.Name+": "+request.Title,
		definition.Profile,
	)
	if err != nil {
		log.Printf("chat api: create agent-k session: %v", err)
		core.WriteError(w, http.StatusBadGateway, "failed to create AgentK session")
		return
	}
	if err := s.agentk.PostMessage(r.Context(), sessionID, request.Message); err != nil {
		log.Printf("chat api: post first message to %s: %v", sessionID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to send first message")
		return
	}
	state := NewChatState()
	state.Status = ChatRunning
	state.AwaitingResponse = true
	now := s.now().UTC()
	chat := Chat{
		ID:        chatID,
		AgentID:   definition.ID,
		SessionID: sessionID,
		Title:     request.Title,
		Status:    ChatRunning,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.chats.Create(chat); err != nil {
		log.Printf("chat api: store chat %s: %v", chat.ID, err)
		core.WriteError(w, http.StatusInternalServerError, "failed to create chat")
		return
	}
	if refreshed, err := s.refreshChat(r.Context(), chat); err == nil {
		chat = refreshed
	}
	core.WriteJSON(w, http.StatusCreated, toChatDTO(chat, true))
}

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	chats := s.chats.List()
	response := make([]chatDTO, 0, len(chats))
	for _, chat := range chats {
		refreshed, err := s.refreshChat(r.Context(), chat)
		if err != nil {
			log.Printf("chat api: refresh %s: %v", chat.ID, err)
			core.WriteError(w, http.StatusBadGateway, "failed to query AgentK")
			return
		}
		response = append(response, toChatDTO(refreshed, false))
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{"chats": response})
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.chats.Get(r.PathValue("chatID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "chat not found")
		return
	}
	refreshed, err := s.refreshChat(r.Context(), chat)
	if err != nil {
		log.Printf("chat api: refresh %s: %v", chat.ID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to query AgentK")
		return
	}
	core.WriteJSON(w, http.StatusOK, toChatDTO(refreshed, true))
}

func (s *Server) handlePostChatMessage(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.chats.Get(r.PathValue("chatID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "chat not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var request postMessageRequest
	if err := core.DecodeOneJSON(r.Body, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" {
		core.WriteError(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := s.agentk.PostMessage(r.Context(), chat.SessionID, request.Text); err != nil {
		log.Printf("chat api: post message to %s: %v", chat.SessionID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to send message")
		return
	}
	state := chat.State.Clone()
	state.AwaitingResponse = true
	state.Status = ChatRunning
	chat, _ = s.chats.UpdateState(chat.ID, state, s.now().UTC())
	if refreshed, err := s.refreshChat(r.Context(), chat); err == nil {
		chat = refreshed
	}
	core.WriteJSON(w, http.StatusAccepted, toChatDTO(chat, true))
}

func (s *Server) handleAnswerChatQuestions(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.chats.Get(r.PathValue("chatID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "chat not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var request answerChatRequest
	if err := core.DecodeOneJSON(r.Body, &request); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	request.ToolCallID = strings.TrimSpace(request.ToolCallID)
	if request.ToolCallID == "" || len(request.Answers) == 0 {
		core.WriteError(w, http.StatusBadRequest, "toolCallId and answers are required")
		return
	}
	for index := range request.Answers {
		request.Answers[index].QuestionID =
			strings.TrimSpace(request.Answers[index].QuestionID)
		if request.Answers[index].QuestionID == "" ||
			len(request.Answers[index].Selected) == 0 {
			core.WriteError(w, http.StatusBadRequest,
				"each answer requires questionId and selected")
			return
		}
		for selectedIndex := range request.Answers[index].Selected {
			request.Answers[index].Selected[selectedIndex] =
				strings.TrimSpace(request.Answers[index].Selected[selectedIndex])
			if request.Answers[index].Selected[selectedIndex] == "" {
				core.WriteError(w, http.StatusBadRequest, "selected values must not be empty")
				return
			}
		}
	}
	if err := s.agentk.AnswerQuestions(
		r.Context(), chat.SessionID, request.ToolCallID, request.Answers,
	); err != nil {
		var upstream *core.AgentKHTTPError
		if errors.As(err, &upstream) &&
			(upstream.StatusCode == http.StatusBadRequest ||
				upstream.StatusCode == http.StatusConflict) {
			core.WriteError(w, upstream.StatusCode, upstream.Message)
			return
		}
		log.Printf("chat api: answer ask_user in %s: %v", chat.SessionID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to submit answers")
		return
	}
	if refreshed, err := s.refreshChat(r.Context(), chat); err == nil {
		chat = refreshed
	} else {
		log.Printf("chat api: refresh %s after answer: %v", chat.ID, err)
	}
	core.WriteJSON(w, http.StatusAccepted, toChatDTO(chat, true))
}

func (s *Server) handleListChatEvents(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.chats.Get(r.PathValue("chatID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "chat not found")
		return
	}
	events, err := s.agentk.ListEvents(r.Context(), chat.SessionID)
	if err != nil {
		core.WriteError(w, http.StatusBadGateway, "failed to query AgentK events")
		return
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	chat, ok := s.chats.Get(r.PathValue("chatID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "chat not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		core.WriteError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(event string, value any) bool {
		raw, err := json.Marshal(value)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	refreshed, err := s.refreshChat(r.Context(), chat)
	if err != nil {
		_ = send("error", map[string]string{"error": "failed to query AgentK"})
		return
	}
	chat = refreshed
	if !send("snapshot", toChatDTO(chat, true)) {
		return
	}

	ticker := time.NewTicker(chatPollInterval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	lastSent := chat.State.LastEventID
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			refreshed, err := s.refreshChat(r.Context(), chat)
			if err != nil {
				_ = send("error", map[string]string{"error": "AgentK stream interrupted"})
				return
			}
			chat = refreshed
			if chat.State.LastEventID > lastSent {
				if !send("snapshot", toChatDTO(chat, true)) {
					return
				}
				lastSent = chat.State.LastEventID
			}
		}
	}
}

func (s *Server) refreshChat(ctx context.Context, chat Chat) (Chat, error) {
	events, err := s.agentk.ListEvents(ctx, chat.SessionID)
	if err != nil {
		return chat, err
	}
	state := chat.State.Clone()
	before := state.LastEventID
	for _, event := range events {
		state.Apply(event)
	}
	if state.LastEventID == before {
		return chat, nil
	}
	return s.chats.UpdateState(chat.ID, state, s.now().UTC())
}

func newChatID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "cht_" + hex.EncodeToString(raw), nil
}
