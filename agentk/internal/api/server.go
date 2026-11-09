// Package api 实现 REST 路由、DTO 映射和 SSE 编码。业务操作由 service 包提供。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/pipeline"
	"agentk/internal/service"
	"agentk/internal/types"
)

// ServerConfig 是 REST handler 的依赖。
type ServerConfig struct {
	Service *service.Service
	// API 和 Worker 必须连接同一个 deltabuf backend。
	Deltas deltabuf.Buffer
}

type Server struct {
	service *service.Service
	deltas  deltabuf.Buffer
}

func NewServer(cfg ServerConfig) *Server {
	if cfg.Service == nil || cfg.Deltas == nil {
		panic("api: service and delta buffer are required")
	}
	return &Server{service: cfg.Service, deltas: cfg.Deltas}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	mux.HandleFunc("POST /v1/sessions/{sessionID}/messages", s.handlePostMessage)
	mux.HandleFunc("GET /v1/sessions/{sessionID}/events", s.handleListEvents)
	mux.HandleFunc("GET /v1/sessions/{sessionID}/stream", s.handleStreamEvents)
	mux.HandleFunc("POST /v1/sessions/{sessionID}/answers", s.handleAnswer)
	mux.HandleFunc("POST /v1/sessions/{sessionID}/cancel", s.handleCancel)
	mux.HandleFunc("GET /v1/sessions/{sessionID}/pipelines", s.handleListPipelines)
	mux.HandleFunc("GET /v1/sessions/{sessionID}/pipeline-runs", s.handleListPipelineRuns)
	mux.HandleFunc("POST /v1/sessions/{sessionID}/pipeline-runs", s.handleStartPipeline)
	mux.HandleFunc("GET /v1/sessions/{sessionID}/pipeline-runs/{execID}", s.handleGetPipelineRun)
	mux.HandleFunc("GET /v1/capabilities", s.handleListCapabilities)
	mux.HandleFunc("GET /v1/me", s.handleWhoAmI)
	registerDocs(mux)
	return mux
}

// maxSessionBody 防止异常请求占用过多内存，不限制 Profile 的业务能力。
const maxSessionBody = 1 << 20

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSessionBody)
	var req struct {
		Title string `json:"title"`
		// Profile 必填；服务端没有默认 Profile。
		Profile json.RawMessage `json:"profile"`
	}
	// 空 body 交给 service 返回缺少 Profile 的错误。
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	sess, err := s.service.CreateSession(r.Context(), p, req.Title, req.Profile)
	if err != nil {
		if errors.Is(err, service.ErrInvalidArgument) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSessionDTO(sess))
}

// handleListSessions 使用 tenant-scoped Session ID 作为 keyset cursor，按创建时间倒序返回。
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), 20, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.service.ListSessions(
		r.Context(), p, r.URL.Query().Get("before"), limit)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]sessionDTO, 0, len(page.Sessions))
	for _, sess := range page.Sessions {
		out = append(out, toSessionDTO(sess))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "hasMore": page.HasMore})
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	text, ok := s.readUserMessage(w, r)
	if !ok {
		return
	}

	sessionID := r.PathValue("sessionID")
	_, err := s.service.SubmitMessage(r.Context(), p, sessionID, text)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrDependencyUnavailable):
			log.Printf("api: post message: %v", err)
			writeError(w, http.StatusBadGateway, "failed to accept message")
		default:
			s.writeStoreError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"sessionId": sessionID,
	})
}

const maxMessageJSONBytes = 1 << 20

func (s *Server) readUserMessage(w http.ResponseWriter, r *http.Request) (string, bool) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if mediaType != "" && mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMessageJSONBytes)
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid json body")
		}
		return "", false
	}
	return req.Text, true
}

// handleListEvents 按存储顺序返回完整的 committed event log。
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	events, err := s.service.ListEvents(r.Context(), p, r.PathValue("sessionID"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": encodeEvents(events),
	})
}

func (s *Server) handleAnswer(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	var req struct {
		ToolCallID string         `json:"toolCallId"`
		Answers    []event.Answer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	sessionID := r.PathValue("sessionID")
	_, err := s.service.SubmitAnswers(
		r.Context(), p, sessionID, req.ToolCallID, req.Answers)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrDependencyUnavailable):
			log.Printf("api: answer session: %v", err)
			writeError(w, http.StatusBadGateway, "failed to accept answer")
		default:
			s.writeStoreError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"sessionId": sessionID,
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	if err := s.service.CancelSession(
		r.Context(), p, r.PathValue("sessionID")); err != nil {
		if errors.Is(err, service.ErrConflict) {
			writeError(w, http.StatusConflict, err.Error())
		} else if errors.Is(err, service.ErrDependencyUnavailable) {
			log.Printf("api: cancel session: %v", err)
			writeError(w, http.StatusBadGateway, "failed to cancel session")
		} else {
			s.writeStoreError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.service.ListPipelines(r.Context(), p, r.PathValue("sessionID"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFeatureDisabled):
			writeError(w, http.StatusNotImplemented, err.Error())
		case errors.Is(err, service.ErrDependencyUnavailable):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		default:
			s.writeStoreError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": items})
}

func (s *Server) handleListPipelineRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	execs, err := s.service.ListExecutions(r.Context(), p, r.PathValue("sessionID"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]pipelineExecDTO, 0, len(execs))
	for _, e := range execs {
		if e.Kind != types.ExecutionKindPipeline {
			continue
		}
		out = append(out, toPipelineExecDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelineRuns": out})
}

func (s *Server) handleStartPipeline(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	var req struct {
		// Pipeline 是 Session Profile 中声明的名称。
		Pipeline string          `json:"pipeline"`
		Args     json.RawMessage `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	exec, err := s.service.StartPipeline(
		r.Context(), p, r.PathValue("sessionID"), req.Pipeline, req.Args)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFeatureDisabled):
			writeError(w, http.StatusNotImplemented, err.Error())
		case errors.Is(err, service.ErrOperationNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrConflict):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, service.ErrDependencyUnavailable):
			if errors.Is(err, pipeline.ErrUnavailable) {
				writeError(w, http.StatusServiceUnavailable, err.Error())
			} else {
				log.Printf("api: start pipeline: %v", err)
				writeError(w, http.StatusBadGateway, "failed to start pipeline")
			}
		case errors.Is(err, service.ErrInvalidArgument):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			s.writeStoreError(w, err)
		}
		return
	}
	writeJSON(w, http.StatusAccepted, toPipelineExecDTO(exec))
}

func (s *Server) handleGetPipelineRun(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	execs, err := s.service.ListExecutions(r.Context(), p, r.PathValue("sessionID"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	for _, exec := range execs {
		if exec.Kind == types.ExecutionKindPipeline && string(exec.ID) == r.PathValue("execID") {
			writeJSON(w, http.StatusOK, toPipelineExecDTO(exec))
			return
		}
	}
	writeError(w, http.StatusNotFound, "pipeline run not found")
}

// Capability catalog 是部署级配置，不按 tenant 过滤。
func (s *Server) handleListCapabilities(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	items, err := s.service.ListCapabilities(r.Context(), p)
	if err != nil {
		if errors.Is(err, service.ErrFeatureDisabled) {
			writeError(w, http.StatusNotImplemented, err.Error())
		} else if errors.Is(err, service.ErrDependencyUnavailable) {
			log.Printf("api: list capabilities: %v", err)
			writeError(w, http.StatusBadGateway, "failed to read capability catalog")
		} else {
			s.writeStoreError(w, err)
		}
		return
	}
	out := make([]capabilityDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toCapabilityDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": out})
}

func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       p.UserPub,
		"tenantId": p.TenantID,
		"display":  p.Display,
	})
}

// requirePrincipal 缺失表示 handler 未经过鉴权中间件，是服务端装配错误。
func (s *Server) requirePrincipal(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	p, ok := authz.FromContext(r.Context())
	if !ok {
		log.Printf("api: %s %s: no principal in context (auth middleware not mounted?)", r.Method, r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal error")
		return authz.Principal{}, false
	}
	return p, true
}

// parseLimit 使用默认值，并将过大的页大小限制为 max。
func parseLimit(raw string, def, max int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	if n > max {
		n = max
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, types.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	log.Printf("api: internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
