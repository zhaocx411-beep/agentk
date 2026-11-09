// 漫剧 API Server：把一次「brief → 成片」封装成 drama 资源。双模式：
//
// pipeline 模式（默认）：每个 drama 绑定一个 AgentK Session 和一次
// comic_drama_film pipeline 执行。创建即启动，状态与资产清单来自 pipeline
// Execution 快照，阶段进度由账本 invocation.progress 事件折叠。需要 Worker 与
// agent-k 连同一 Temporal 集群。
//
// agent 模式：brief 作为第一条用户消息驱动自由 Agent 会话（profile-agent），
// 状态由账本整体折叠（fold_agent.go），支持 /messages 继续对话。只依赖
// AgentK Connect API，无 Worker。
package comicdrama

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"studio/platform/core"
)

// maxShotsLimit 每集分镜数上限、maxEpisodesLimit 集数上限
// （与 worker 的 hardMaxShots / hardMaxEpisodes 一致，对齐 qai）。
const (
	maxShotsLimit    = 10
	maxEpisodesLimit = 10
)

type Server struct {
	mode    core.DramaMode
	agentk  core.AgentKClient
	store   *MemoryDramaStore
	profile json.RawMessage
	models  core.ModelRefs
	now     func() time.Time
	newID   func() (string, error)
}

type createDramaRequest struct {
	// Brief 题材、梗概或完整剧本。
	Brief string `json:"brief"`
	Title string `json:"title"`
	// StylePrompt 全剧统一视觉风格锚点（前端预设风格或自定义文本）。
	// agent 模式下附加到 brief 末尾；pipeline 模式下注入各生成阶段。
	StylePrompt string `json:"stylePrompt"`
	// Episodes 目标集数，0 取 worker 默认值 1（仅 pipeline 模式）。
	Episodes int `json:"episodes"`
	// MaxShots 每集分镜数上限，0 取 worker 默认值（仅 pipeline 模式）。
	MaxShots int `json:"maxShots"`
}

type postMessageRequest struct {
	Text string `json:"text"`
}

type dramaDTO struct {
	ID string `json:"id"`
	// Mode 服务运行模式（pipeline / agent），前端据此选择渲染方式。
	Mode   core.DramaMode `json:"mode"`
	Status DramaStatus    `json:"status"`
	Title  string         `json:"title"`
	Brief  string         `json:"brief"`

	// —— pipeline 模式 ——
	// Progress 各阶段的进度汇报（按发生序）。
	Progress []ProgressStep `json:"progress,omitempty"`
	// Output 完成时的完整资产清单：剧本、人物定妆、逐镜首帧与片段、成片。
	Output *core.DramaManifest `json:"output,omitempty"`

	// —— agent 模式 ——
	// AssistantSays 模型最近一轮的完整正文。
	AssistantSays string `json:"assistantSays,omitempty"`
	// Messages 完整会话往来（用户指令 + 模型各轮正文），时间序。
	Messages []messageDTO `json:"messages,omitempty"`
	// Stages 画布五阶段的状态与各自产物（见 stages.go）。
	Stages []stageDTO `json:"stages,omitempty"`
	// Assets 全会话产物清单（URI 去重，时间序）。
	Assets []assetDTO `json:"assets,omitempty"`
	// Edges 生成血缘：from 资产作为输入参与生成了 to 资产。
	Edges []edgeDTO `json:"edges,omitempty"`
	// Pending 在途生成：画布上的"生成中"占位节点及其输入。
	Pending []pendingDTO `json:"pending,omitempty"`
	// Films 合成工具产出的成片 URL，时间序。
	Films      []string `json:"films,omitempty"`
	ImageCount int      `json:"imageCount,omitempty"`
	VideoCount int      `json:"videoCount,omitempty"`

	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func NewServer(
	mode core.DramaMode,
	agentk core.AgentKClient,
	store *MemoryDramaStore,
	profile json.RawMessage,
	models core.ModelRefs,
) *Server {
	return &Server{
		mode:    mode,
		agentk:  agentk,
		store:   store,
		profile: append(json.RawMessage(nil), profile...),
		models:  models,
		now:     time.Now,
		newID:   newDramaID,
	}
}

// Register 把漫剧路由挂到 mux 上；健康检查、前端与其他 Agent 的路由由
// platform/app 统一组装。
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/dramas", s.handleCreateDrama)
	mux.HandleFunc("GET /v1/dramas", s.handleListDramas)
	mux.HandleFunc("GET /v1/dramas/{dramaID}", s.handleGetDrama)
	mux.HandleFunc("GET /v1/dramas/{dramaID}/events", s.handleListEvents)
	if s.mode == core.ModeAgent {
		// 继续对话只在 agent 模式存在：pipeline 是一次性执行，没有会话通道。
		mux.HandleFunc("POST /v1/dramas/{dramaID}/messages", s.handlePostMessage)
	}
}

// Handler 返回只含漫剧路由的 http.Handler（测试与单独部署用）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

func (s *Server) handleCreateDrama(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var req createDramaRequest
	if err := core.DecodeOneJSON(r.Body, &req); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Brief = strings.TrimSpace(req.Brief)
	req.Title = strings.TrimSpace(req.Title)
	req.StylePrompt = strings.TrimSpace(req.StylePrompt)
	if req.Brief == "" {
		core.WriteError(w, http.StatusBadRequest, "brief is required")
		return
	}
	if req.MaxShots < 0 || req.MaxShots > maxShotsLimit {
		core.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("maxShots must be between 0 and %d", maxShotsLimit))
		return
	}
	if req.Episodes < 0 || req.Episodes > maxEpisodesLimit {
		core.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("episodes must be between 0 and %d", maxEpisodesLimit))
		return
	}
	displayTitle := req.Title
	if displayTitle == "" {
		displayTitle = core.TruncateRunes(req.Brief, 24)
	}

	dramaID, err := s.newID()
	if err != nil {
		log.Printf("api: generate drama id: %v", err)
		core.WriteError(w, http.StatusInternalServerError, "failed to create drama")
		return
	}
	sessionID, err := s.agentk.CreateSession(r.Context(), "漫剧: "+displayTitle, s.profile)
	if err != nil {
		log.Printf("api: create agent-k session: %v", err)
		core.WriteError(w, http.StatusBadGateway, "failed to create AgentK session")
		return
	}

	drama := Drama{
		ID:          dramaID,
		SessionID:   sessionID,
		Title:       displayTitle,
		Brief:       req.Brief,
		StylePrompt: req.StylePrompt,
		Status:      DramaRunning,
		CreatedAt:   s.now().UTC(),
		UpdatedAt:   s.now().UTC(),
	}

	switch s.mode {
	case core.ModePipeline:
		execID, err := s.agentk.StartPipeline(r.Context(), sessionID, core.DramaArgs{
			Brief:       req.Brief,
			Title:       req.Title,
			StylePrompt: req.StylePrompt,
			Episodes:    req.Episodes,
			MaxShots:    req.MaxShots,
			ChatModel:   s.models.Chat,
			ImageModel:  s.models.Image,
			EditModel:   s.models.Edit,
			VideoModel:  s.models.Video,
		})
		if err != nil {
			log.Printf("api: start pipeline in session %s: %v", sessionID, err)
			core.WriteError(w, http.StatusBadGateway, "failed to start AgentK pipeline")
			return
		}
		drama.ExecID = execID

	case core.ModeAgent:
		// 风格锚点附加到 brief 末尾（与旧版前端拼接方式一致），
		// 作为第一条用户消息驱动 Agent。
		message := req.Brief
		if req.StylePrompt != "" {
			message += "\n\n【全剧统一视觉风格】" + req.StylePrompt +
				"\n所有人物、场景、道具、首帧和视频镜头必须保持这一风格。"
		}
		if err := s.agentk.PostMessage(r.Context(), sessionID, message); err != nil {
			log.Printf("api: post first message to session %s: %v", sessionID, err)
			core.WriteError(w, http.StatusBadGateway, "failed to send brief to AgentK")
			return
		}
		state := NewDramaState()
		// 首条消息已被 AgentK 受理：即使初次刷新失败，状态也不该回落成
		// idle——账本里的 user.input 会在下次刷新时兑现同一事实。
		state.Converged = false
		drama.State = state
	}

	if err := s.store.Create(drama); err != nil {
		log.Printf("api: store drama %s: %v", drama.ID, err)
		core.WriteError(w, http.StatusInternalServerError, "failed to create drama")
		return
	}
	core.WriteJSON(w, http.StatusAccepted, s.toDTO(drama))
}

// handlePostMessage（仅 agent 模式）向漫剧会话追加一条用户消息：
// 追加下一集、修改设定、要求重做等。AgentK 忙时消息排队。
func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	drama, ok := s.store.Get(r.PathValue("dramaID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "drama not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxRequestBody)
	var req postMessageRequest
	if err := core.DecodeOneJSON(r.Body, &req); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		core.WriteError(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := s.agentk.PostMessage(r.Context(), drama.SessionID, req.Text); err != nil {
		log.Printf("api: post message to session %s: %v", drama.SessionID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to send message to AgentK")
		return
	}
	drama, err := s.refreshDrama(r.Context(), drama)
	if err != nil {
		log.Printf("api: refresh drama %s: %v", drama.ID, err)
	}
	core.WriteJSON(w, http.StatusAccepted, s.toDTO(drama))
}

func (s *Server) handleGetDrama(w http.ResponseWriter, r *http.Request) {
	drama, ok := s.store.Get(r.PathValue("dramaID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "drama not found")
		return
	}
	drama, err := s.refreshDrama(r.Context(), drama)
	if err != nil {
		log.Printf("api: refresh drama %s: %v", drama.ID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to query AgentK")
		return
	}
	core.WriteJSON(w, http.StatusOK, s.toDTO(drama))
}

func (s *Server) handleListDramas(w http.ResponseWriter, r *http.Request) {
	dramas := s.store.List()
	response := make([]dramaDTO, 0, len(dramas))
	for _, drama := range dramas {
		refreshed, err := s.refreshDrama(r.Context(), drama)
		if err != nil {
			log.Printf("api: refresh drama %s: %v", drama.ID, err)
			// One deleted or temporarily unavailable AgentK session must not
			// hide every healthy project in the list. Keep the cached snapshot
			// for this entry and continue refreshing the rest.
			response = append(response, s.toDTO(drama))
			continue
		}
		response = append(response, s.toDTO(refreshed))
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{"dramas": response})
}

// handleListEvents 原样代理完整会话账本，供需要完整过程（进度明细、
// 用量）的客户端消费；Studio 对外一次返回全量，内部已遍历 AgentK 分页。
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	drama, ok := s.store.Get(r.PathValue("dramaID"))
	if !ok {
		core.WriteError(w, http.StatusNotFound, "drama not found")
		return
	}
	events, err := s.agentk.ListEvents(r.Context(), drama.SessionID)
	if err != nil {
		log.Printf("api: list events of drama %s: %v", drama.ID, err)
		core.WriteError(w, http.StatusBadGateway, "failed to query AgentK events")
		return
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

// refreshDrama 按模式刷新一个 drama 的现场。
func (s *Server) refreshDrama(ctx context.Context, drama Drama) (Drama, error) {
	if s.mode == core.ModeAgent {
		return s.refreshAgent(ctx, drama)
	}
	return s.refreshPipeline(ctx, drama)
}

// refreshPipeline 终态后不再回源；运行中先查 pipeline run 快照拿状态与
// 最终清单，再把 LastEventID 之后的账本增量折叠成阶段进度。
func (s *Server) refreshPipeline(ctx context.Context, drama Drama) (Drama, error) {
	if drama.Status == DramaCompleted || drama.Status == DramaFailed ||
		drama.Status == DramaCancelled {
		return drama, nil
	}
	snapshot, err := s.agentk.GetPipelineRun(ctx, drama.SessionID, drama.ExecID)
	if err != nil {
		return drama, err
	}

	events, err := s.agentk.ListEvents(ctx, drama.SessionID)
	if err != nil {
		return drama, err
	}
	// /events 返回全量日志：只折叠游标之后的增量。
	lastEventID := drama.LastEventID
	fresh := events[:0:0]
	for _, ev := range events {
		if ev.ID <= drama.LastEventID {
			continue
		}
		fresh = append(fresh, ev)
		if ev.ID > lastEventID {
			lastEventID = ev.ID
		}
	}
	progress := foldProgress(
		append([]ProgressStep(nil), drama.Progress...), drama.ExecID, fresh,
	)
	return s.store.UpdateSnapshot(
		drama.ID,
		DramaStatus(snapshot.Status),
		snapshot.Output,
		snapshot.Error,
		progress,
		lastEventID,
		s.now().UTC(),
	)
}

// refreshAgent 拉取完整账本并折叠进 DramaState（Apply 会按事件 ID 跳过
// 已折叠的前缀）。Agent 会话没有终态：idle 后还能继续对话，所以每次都回源。
func (s *Server) refreshAgent(ctx context.Context, drama Drama) (Drama, error) {
	events, err := s.agentk.ListEvents(ctx, drama.SessionID)
	if err != nil {
		return drama, err
	}
	state := drama.State.Clone()
	before := state.LastEventID
	for _, ev := range events {
		state.Apply(ev)
	}
	if state.LastEventID == before {
		return drama, nil
	}
	return s.store.UpdateState(drama.ID, state, s.now().UTC())
}

func newDramaID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "drm_" + hex.EncodeToString(raw), nil
}

func (s *Server) toDTO(drama Drama) dramaDTO {
	dto := dramaDTO{
		ID:        drama.ID,
		Mode:      s.mode,
		Status:    drama.Status,
		Title:     drama.Title,
		Brief:     drama.Brief,
		Error:     drama.Error,
		CreatedAt: drama.CreatedAt,
		UpdatedAt: drama.UpdatedAt,
	}
	switch s.mode {
	case core.ModePipeline:
		dto.Progress = drama.Progress
		if dto.Progress == nil {
			dto.Progress = []ProgressStep{}
		}
		dto.Output = drama.Manifest
	case core.ModeAgent:
		dto.Status = drama.State.Status()
		dto.AssistantSays = drama.State.AssistantSays
		dto.Messages = make([]messageDTO, 0, len(drama.State.Messages))
		for _, message := range drama.State.Messages {
			dto.Messages = append(dto.Messages, messageDTO{
				Role: message.Role, Text: message.Text, At: message.At,
			})
		}
		dto.Stages = agentStages(drama.State, dto.Status)
		dto.Assets = make([]assetDTO, 0, len(drama.State.Assets))
		for _, asset := range drama.State.Assets {
			dto.Assets = append(dto.Assets, assetDTO{
				Kind: asset.Kind, URI: asset.URI, Tool: asset.Tool,
				Stage: stageForAsset(asset), Description: asset.Description,
				At: asset.At,
			})
		}
		dto.Edges = make([]edgeDTO, 0, len(drama.State.Edges))
		for _, edge := range drama.State.Edges {
			dto.Edges = append(dto.Edges, edgeDTO{From: edge.From, To: edge.To})
		}
		if dto.Status == DramaRunning {
			dto.Pending = agentPending(drama.State)
		}
		dto.Films = append([]string(nil), drama.State.Films...)
		dto.ImageCount = drama.State.ImageCount
		dto.VideoCount = drama.State.VideoCount
		// 只在折叠判定为终态 error 时下发错误文案：loop 被打断但异步
		// 任务仍在跑（会自动唤醒）的场景，前端不该弹"任务中断"。
		if dto.Status == DramaError {
			dto.Error = drama.State.ErrorMessage
		}
	}
	return dto
}
