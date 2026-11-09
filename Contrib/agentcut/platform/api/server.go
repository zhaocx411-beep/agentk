// Package api 实现 cut-apiserver 的 HTTP 层：终端用户入口、项目管理、
// Timeline Ops 网关、素材上传（服务端中转）、agent-k 对话代理。
//
// 权威状态全部在 Temporal 实体 Workflow 里，本包只做鉴权、解析、转发，
// 不落任何本地状态（MVP 无登录体系，用户标识取 X-AgentCut-User 头，
// 缺省 dev，部署在可信内网）。
package api

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

	"connectrpc.com/connect"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/agentkclient"
	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/render"
	"agentcut/timeline"
)

// TemporalClient 是 Server 依赖的最小 Temporal 客户端切面。
type TemporalClient interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow any, args ...any) (client.WorkflowRun, error)
	UpdateWorkflow(ctx context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error)
	QueryWorkflow(ctx context.Context, workflowID string, runID string, queryType string, args ...any) (converter.EncodedValue, error)
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg any) error
	DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	GetWorkflow(ctx context.Context, workflowID, runID string) client.WorkflowRun
}

// Server 承载全部路由。AgentK / Blob 允许为 nil（对应功能返回 503，方便
// 无外部依赖的本地开发）。
type Server struct {
	Temporal        TemporalClient
	Blob            blob.Storage
	AgentK          *agentkclient.Client
	Prober          platformmedia.Prober
	AudioPreviewer  platformmedia.AudioPreviewer
	Profile         []byte // Session Profile 模板（agents/agentcut/profile.json）
	Profiles        []ProfileDefinition
	TaskQueue       string // 实体 Workflow 与媒体预处理 Workflow 的 Task Queue
	Auth            AuthConfig
	RenderSelection render.Selection
}

// Handler 返回完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/profiles", s.handleListProfiles)
	mux.HandleFunc("GET /v1/agent-profiles", s.handleListProfiles)

	mux.HandleFunc("GET /v1/projects", s.handleListProjects)
	mux.HandleFunc("POST /v1/projects", s.handleCreateProject)
	mux.HandleFunc("PATCH /v1/projects/{projectID}", s.handleRenameProject)
	mux.HandleFunc("DELETE /v1/projects/{projectID}", s.handleDeleteProject)
	mux.HandleFunc("POST /v1/projects/{projectID}/shares", s.handleCreateShare)
	mux.HandleFunc("POST /v1/projects/{projectID}/share", s.handleCreateShare)
	mux.HandleFunc("DELETE /v1/projects/{projectID}/shares/{token}", s.handleRevokeShare)
	mux.HandleFunc("GET /v1/shared/{token}/timeline", s.handleGetSharedTimeline)

	mux.HandleFunc("GET /v1/projects/{projectID}/timeline", s.handleGetTimeline)
	mux.HandleFunc("POST /v1/projects/{projectID}/timelines", s.handleCreateTimeline)
	mux.HandleFunc("POST /v1/projects/{projectID}/timelines/{timelineID}/activate", s.handleSwitchTimeline)
	mux.HandleFunc("PATCH /v1/projects/{projectID}/timelines/{timelineID}", s.handleRenameTimeline)
	mux.HandleFunc("DELETE /v1/projects/{projectID}/timelines/{timelineID}", s.handleDeleteTimeline)
	mux.HandleFunc("PATCH /v1/projects/{projectID}/timelines/{timelineID}/view", s.handleUpdateTimelineView)
	mux.HandleFunc("POST /v1/projects/{projectID}/timeline/ops", s.handleTimelineOps)
	mux.HandleFunc("POST /v1/projects/{projectID}/timeline:restore", s.handleRestoreTimeline)
	mux.HandleFunc("GET /v1/projects/{projectID}/transcript-view", s.handleGetTranscriptView)
	mux.HandleFunc("POST /v1/projects/{projectID}/transcript-edits:delete", s.handleDeleteTranscriptRanges)

	mux.HandleFunc("POST /v1/projects/{projectID}/uploads", s.handleUpload)
	mux.HandleFunc("GET /v1/projects/{projectID}/media/{mediaID}/content", s.handleMediaContent)
	mux.HandleFunc("GET /v1/projects/{projectID}/media/{mediaID}/playback", s.handleMediaPlayback)
	mux.HandleFunc("GET /v1/projects/{projectID}/media/{mediaID}/thumbnail", s.handleMediaThumbnail)
	mux.HandleFunc("POST /v1/projects/{projectID}/media/{mediaID}/prepare", s.handlePrepareMedia)
	mux.HandleFunc("POST /v1/projects/{projectID}/items/{itemID}/audio-separation", s.handleStartSeparateAudio)
	mux.HandleFunc("GET /v1/projects/{projectID}/items/{itemID}/audio-separation", s.handleGetSeparateAudio)

	mux.HandleFunc("POST /v1/projects/{projectID}/messages", s.handleAgentMessage)
	mux.HandleFunc("GET /v1/projects/{projectID}/events", s.handleAgentEvents)
	mux.HandleFunc("GET /v1/projects/{projectID}/stream", s.handleAgentStream)
	mux.HandleFunc("POST /v1/projects/{projectID}/answers", s.handleAgentAnswers)

	mux.HandleFunc("POST /v1/projects/{projectID}/renders", s.handleStartRender)
	mux.HandleFunc("GET /v1/projects/{projectID}/renders/{renderKey}", s.handleRenderStatus)
	mux.HandleFunc("GET /v1/projects/{projectID}/renders/{renderKey}/content", s.handleRenderContent)
	return s.Auth.middleware(mux)
}

// userOf 只读取认证中间件写入的不可变 context，不再直接信任请求头。
func userOf(r *http.Request) string {
	return userFromContext(r.Context())
}

func newID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

// ---- 项目 ----

func (s *Server) listProjects(ctx context.Context, user string) ([]entity.ProjectRef, error) {
	ev, err := s.Temporal.QueryWorkflow(ctx, entity.UserProjectsWorkflowID(user), "", entity.QueryListProjects)
	if err != nil {
		return nil, err
	}
	var projects []entity.ProjectRef
	if err := ev.Get(&projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// isNotFound 判断错误是否为"Workflow 不存在"（该用户还没有任何项目 /
// 项目已删除）。
func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.listProjects(r.Context(), userOf(r))
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"projects": []entity.ProjectRef{}})
			return
		}
		writeError(w, http.StatusBadGateway, "failed to list projects")
		return
	}
	if projects == nil {
		projects = []entity.ProjectRef{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

type createProjectRequest struct {
	Title     string `json:"title"`
	ProfileID string `json:"profileId,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	FPS       int    `json:"fps,omitempty"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Title == "" {
		req.Title = "未命名项目"
	}
	if req.Width == 0 {
		req.Width = 1920
	}
	if req.Height == 0 {
		req.Height = 1080
	}
	if req.FPS == 0 {
		req.FPS = timeline.DefaultFPS
	}
	if !timeline.AllowedFPS[req.FPS] {
		writeError(w, http.StatusBadRequest, "fps must be one of 24/25/30/50/60")
		return
	}
	profileDef, ok := s.resolveProfile(req.ProfileID)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "unknown profileId",
			"code":  "PROFILE_NOT_FOUND",
		})
		return
	}
	projectID := newID("prj")

	// 0. 纯计算的准备放在启动 Workflow 之前，失败无需补偿。
	var profile *apiv1.Profile
	if s.AgentK != nil {
		var perr error
		profile, perr = BuildSessionProfile(profileDef.ProfileJSON, projectID)
		if perr != nil {
			writeError(w, http.StatusInternalServerError, "invalid profile template")
			return
		}
	}

	// 1. 起 Timeline 实体 Workflow（幂等：已存在则直接返回现有 Run）。
	// 这是长驻 Workflow：从这一步起，后续任何失败都必须补偿删除它，
	// 否则会永久泄漏（未登记到任何用户，无人能删）。
	doc := timeline.NewDoc(req.Title, req.Width, req.Height, req.FPS)
	timelineID := newID("tl")
	doc.Tracks = []timeline.Track{{
		TrackID: newID("trk"), Kind: timeline.TrackVideo, Name: "视频轨",
		CreatedBy: timeline.TrackCreatedAuto,
	}}
	nowUnixMs := time.Now().UnixMilli()
	_, err := s.Temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        entity.TimelineWorkflowID(projectID),
		TaskQueue: s.TaskQueue,
	}, entity.TimelineWorkflowName, entity.TimelineInit{
		ProjectID: projectID, Doc: doc, ActiveTimelineID: timelineID,
		Timelines: []entity.TimelineSequence{{
			TimelineID: timelineID, Name: entity.DefaultSequenceName, Doc: doc, Zoom: 1,
			CreatedAtUnixMs: nowUnixMs, UpdatedAtUnixMs: nowUnixMs,
		}},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start timeline workflow")
		return
	}

	// 2. 创建 agent-k Session（未配置 agent-k 时跳过，项目仍可手动剪辑）。
	sessionID := ""
	if s.AgentK != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		sessionID, err = s.AgentK.CreateSession(ctx, "AgentCut: "+req.Title, profile)
		if err != nil {
			s.compensateTimeline(r.Context(), projectID, "create agent session failed")
			writeError(w, http.StatusBadGateway, "failed to create agent session: "+err.Error())
			return
		}
		if err := s.bindTimelineAgentKSession(ctx, projectID, sessionID); err != nil {
			s.compensateTimeline(r.Context(), projectID, "bind agent session failed")
			s.compensateAgentSession(r.Context(), sessionID, projectID, user, "bind timeline failed")
			writeError(w, http.StatusBadGateway, "failed to bind project agent session")
			return
		}
	}

	// 3. 登记进用户项目索引（先确保索引 Workflow 存在，再幂等 Update）。
	ref := entity.ProjectRef{
		ProjectID:       projectID,
		SessionID:       sessionID,
		ProfileID:       profileDef.ProfileID,
		Title:           req.Title,
		CreatedAtUnixMs: time.Now().UnixMilli(),
	}
	if err := s.addProjectRef(r.Context(), user, ref); err != nil {
		s.compensateTimeline(r.Context(), projectID, "register project failed")
		if sessionID != "" {
			s.compensateAgentSession(r.Context(), sessionID, projectID, user, "register project failed")
		}
		writeError(w, http.StatusBadGateway, "failed to register project")
		return
	}
	writeJSON(w, http.StatusCreated, ref)
}

// compensateAgentSession 尽力取消 Session 中的活动 Execution。agent-k 当前
// 没有 DeleteSession RPC，因此新建后仍空闲的 Session 无法从其存储中删除；
// 结构化日志用于后续审计和真正删除 API 上线后的补偿任务。
func (s *Server) compensateAgentSession(parent context.Context, sessionID, projectID, user, cause string) {
	if s.AgentK == nil || sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	err := s.AgentK.CancelExecution(ctx, sessionID)
	if err == nil {
		log.Printf("INFO: agent-k execution cancelled sessionId=%s projectId=%s user=%q cause=%q",
			sessionID, projectID, user, cause)
		return
	}
	if connect.CodeOf(err) == connect.CodeFailedPrecondition {
		log.Printf("WARN: orphan agent-k session is idle and cannot be deleted sessionId=%s projectId=%s user=%q cause=%q",
			sessionID, projectID, user, cause)
		return
	}
	log.Printf("ERROR: agent-k session compensation failed sessionId=%s projectId=%s user=%q cause=%q error=%v",
		sessionID, projectID, user, cause, err)
}

// compensateTimeline 尽力删除创建到一半的 Timeline 实体 Workflow。
// 用不随请求取消的 context：走到补偿往往正是因为请求 context 已经
// 取消/超时，不能再用它发信号。补偿失败只记日志——实体 Workflow ID
// 含 projectID，可凭日志人工清理。
func (s *Server) compensateTimeline(parent context.Context, projectID, cause string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	if err := s.Temporal.SignalWorkflow(ctx, entity.TimelineWorkflowID(projectID), "", entity.SignalDeleteTimeline, nil); err != nil && !isNotFound(err) {
		log.Printf("ERROR: leaked timeline workflow %s (%s; compensation failed: %v)",
			entity.TimelineWorkflowID(projectID), cause, err)
	}
}

func (s *Server) addProjectRef(ctx context.Context, user string, ref entity.ProjectRef) error {
	if _, err := s.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        entity.UserProjectsWorkflowID(user),
		TaskQueue: s.TaskQueue,
	}, entity.UserProjectsWorkflowName, entity.UserProjectsInit{UserSubject: user}); err != nil {
		return err
	}
	handle, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   entity.UserProjectsWorkflowID(user),
		UpdateName:   entity.UpdateAddProject,
		UpdateID:     "add:" + ref.ProjectID,
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{entity.AddProjectRequest{Ref: ref}},
	})
	if err != nil {
		return err
	}
	var out entity.ProjectRef
	return handle.Get(ctx, &out)
}

// requireProject 校验项目归属，返回项目记录。
func (s *Server) requireProject(w http.ResponseWriter, r *http.Request) (entity.ProjectRef, bool) {
	projectID := r.PathValue("projectID")
	projects, err := s.listProjects(r.Context(), userOf(r))
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			writeError(w, http.StatusBadGateway, "failed to check project ownership")
		}
		return entity.ProjectRef{}, false
	}
	for _, p := range projects {
		if p.ProjectID == projectID {
			return p, true
		}
	}
	writeError(w, http.StatusNotFound, "project not found")
	return entity.ProjectRef{}, false
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	user := userOf(r)
	if err := s.Temporal.SignalWorkflow(r.Context(), entity.TimelineWorkflowID(ref.ProjectID), "", entity.SignalDeleteTimeline, nil); err != nil && !isNotFound(err) {
		writeError(w, http.StatusBadGateway, "failed to delete timeline")
		return
	}
	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   entity.UserProjectsWorkflowID(user),
		UpdateName:   entity.UpdateRemoveProject,
		UpdateID:     "remove:" + ref.ProjectID,
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{entity.RemoveProjectRequest{ProjectID: ref.ProjectID}},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to unregister project")
		return
	}
	if err := handle.Get(r.Context(), nil); err != nil {
		writeError(w, http.StatusBadGateway, "failed to unregister project")
		return
	}
	s.compensateAgentSession(r.Context(), ref.SessionID, ref.ProjectID, user, "project deleted")
	w.WriteHeader(http.StatusNoContent)
}

type renameProjectRequest struct {
	Title     string `json:"title"`
	RequestID string `json:"requestId"`
}

// handleRenameProject 同步更新用户项目索引标题与 Timeline metadata.name。
// 两个 Temporal Entity 无跨 Workflow 事务；索引 Update 先落地，Timeline 遇到
// 并发 revision 最多重试三次，保证正常并发下最终一致。
func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	var req renameProjectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len([]rune(req.Title)) > 120 {
		writeError(w, http.StatusBadRequest, "title must contain 1..120 characters")
		return
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "requestId is required (idempotency key)")
		return
	}

	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   entity.UserProjectsWorkflowID(userOf(r)),
		UpdateName:   entity.UpdateRenameProject,
		UpdateID:     "rename:" + req.RequestID,
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []any{entity.RenameProjectRequest{
			ProjectID: ref.ProjectID,
			Title:     req.Title,
		}},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to rename project")
		return
	}
	var renamed entity.ProjectRef
	if err := handle.Get(r.Context(), &renamed); err != nil {
		writeError(w, http.StatusBadGateway, "failed to rename project")
		return
	}

	var syncErr *httpError
	for attempt := 0; attempt < 3; attempt++ {
		state, err := s.getTimelineState(r.Context(), ref.ProjectID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "project renamed but timeline title sync failed")
			return
		}
		name := req.Title
		_, syncErr = s.applyOps(
			r.Context(),
			ref.ProjectID,
			fmt.Sprintf("rename-meta:%s:%d", req.RequestID, attempt),
			"user",
			state.Revision,
			[]timeline.Operation{{
				Op:       timeline.OpSetMetadata,
				Metadata: &timeline.MetadataPatch{Name: &name},
			}},
		)
		if syncErr == nil {
			writeJSON(w, http.StatusOK, renamed)
			return
		}
		if syncErr.status != http.StatusConflict {
			break
		}
	}
	writeError(w, http.StatusBadGateway, "project renamed but timeline title sync failed")
}

// ---- Timeline ----

func (s *Server) getTimelineState(ctx context.Context, projectID string) (entity.TimelineState, error) {
	ev, err := s.Temporal.QueryWorkflow(ctx, entity.TimelineWorkflowID(projectID), "", entity.QueryGetState)
	if err != nil {
		return entity.TimelineState{}, err
	}
	var st entity.TimelineState
	if err := ev.Get(&st); err != nil {
		return entity.TimelineState{}, err
	}
	return st, nil
}

func (s *Server) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	st, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	etag := fmt.Sprintf(`"timeline-%s-%d-%d"`, st.ActiveTimelineID, st.Revision, st.ProjectRevision)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, timelineStatePayload(st))
}

func timelineStatePayload(st entity.TimelineState) map[string]any {
	return map[string]any{
		"revision":         st.Revision,
		"doc":              st.Doc,
		"durationInFrames": timeline.DocDurationInFrames(st.Doc),
		"activeTimelineId": st.ActiveTimelineID,
		"timelines":        st.TimelineSummaries(),
		"projectRevision":  st.ProjectRevision,
	}
}

type timelineMutationRequest struct {
	RequestID string `json:"requestId"`
	Name      string `json:"name,omitempty"`
}

func (s *Server) updateTimelineLifecycle(
	ctx context.Context,
	projectID string,
	timelineID string,
	requestID string,
	updateName string,
	arg any,
) (entity.TimelineState, *httpError) {
	if err := entity.ValidateClientMutationID(requestID); err != nil {
		return entity.TimelineState{}, &httpError{http.StatusBadRequest, map[string]any{
			"code": "BAD_REQUEST", "error": err.Error(),
		}}
	}
	handle, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(projectID),
		UpdateName:   updateName,
		UpdateID:     entity.TimelineSequenceMutationUpdateID(projectID, timelineID, updateName, requestID),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{arg},
	})
	if err != nil {
		return entity.TimelineState{}, &httpError{http.StatusBadGateway, map[string]any{
			"code": "TIMELINE_UNAVAILABLE", "error": "failed to update timelines",
		}}
	}
	var state entity.TimelineState
	if err := handle.Get(ctx, &state); err != nil {
		message := err.Error()
		status := http.StatusUnprocessableEntity
		switch {
		case strings.Contains(message, "not found"):
			status = http.StatusNotFound
		case strings.Contains(message, "cannot be deleted"), strings.Contains(message, "already exists"):
			status = http.StatusConflict
		}
		return entity.TimelineState{}, &httpError{status, map[string]any{
			"code": "TIMELINE_UPDATE_FAILED", "error": message,
		}}
	}
	return state, nil
}

func decodeTimelineMutationRequest(w http.ResponseWriter, r *http.Request) (timelineMutationRequest, bool) {
	var req timelineMutationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return timelineMutationRequest{}, false
	}
	return req, true
}

func (s *Server) handleCreateTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	req, ok := decodeTimelineMutationRequest(w, r)
	if !ok {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		state, err := s.getTimelineState(r.Context(), ref.ProjectID)
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, "TIMELINE_UNAVAILABLE", "failed to load timelines")
			return
		}
		req.Name = fmt.Sprintf("剪辑 %d", len(state.Timelines)+1)
	}
	timelineID := newID("tl")
	state, httpErr := s.updateTimelineLifecycle(
		r.Context(), ref.ProjectID, timelineID, req.RequestID, entity.UpdateCreateTimeline,
		entity.CreateTimelineRequest{
			TimelineID: timelineID, Name: req.Name, BaseVideoTrackID: newID("trk"),
			CreatedAtUnixMs: time.Now().UnixMilli(),
		},
	)
	if httpErr != nil {
		httpErr.write(w)
		return
	}
	writeJSON(w, http.StatusCreated, timelineStatePayload(state))
}

func (s *Server) handleSwitchTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	req, ok := decodeTimelineMutationRequest(w, r)
	if !ok {
		return
	}
	timelineID := r.PathValue("timelineID")
	state, httpErr := s.updateTimelineLifecycle(
		r.Context(), ref.ProjectID, timelineID, req.RequestID, entity.UpdateSwitchTimeline,
		entity.SwitchTimelineRequest{TimelineID: timelineID, UpdatedAtUnixMs: time.Now().UnixMilli()},
	)
	if httpErr != nil {
		httpErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, timelineStatePayload(state))
}

func (s *Server) handleRenameTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	req, ok := decodeTimelineMutationRequest(w, r)
	if !ok {
		return
	}
	timelineID := r.PathValue("timelineID")
	state, httpErr := s.updateTimelineLifecycle(
		r.Context(), ref.ProjectID, timelineID, req.RequestID, entity.UpdateRenameTimeline,
		entity.RenameTimelineRequest{
			TimelineID: timelineID, Name: req.Name, UpdatedAtUnixMs: time.Now().UnixMilli(),
		},
	)
	if httpErr != nil {
		httpErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, timelineStatePayload(state))
}

func (s *Server) handleDeleteTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	req, ok := decodeTimelineMutationRequest(w, r)
	if !ok {
		return
	}
	timelineID := r.PathValue("timelineID")
	state, httpErr := s.updateTimelineLifecycle(
		r.Context(), ref.ProjectID, timelineID, req.RequestID, entity.UpdateDeleteTimeline,
		entity.DeleteTimelineRequest{TimelineID: timelineID, UpdatedAtUnixMs: time.Now().UnixMilli()},
	)
	if httpErr != nil {
		httpErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, timelineStatePayload(state))
}

type updateTimelineViewRequest struct {
	RequestID    string  `json:"requestId"`
	CurrentFrame int64   `json:"currentFrame"`
	Zoom         float64 `json:"zoom"`
}

func (s *Server) handleUpdateTimelineView(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	var req updateTimelineViewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := entity.ValidateClientMutationID(req.RequestID); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	timelineID := r.PathValue("timelineID")
	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(ref.ProjectID),
		UpdateName:   entity.UpdateTimelineView,
		UpdateID:     entity.TimelineSequenceMutationUpdateID(ref.ProjectID, timelineID, "view", req.RequestID),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []any{entity.UpdateTimelineViewRequest{
			TimelineID: timelineID, CurrentFrame: req.CurrentFrame, Zoom: req.Zoom,
			UpdatedAtUnixMs: time.Now().UnixMilli(),
		}},
	})
	if err != nil {
		writeStructuredError(w, http.StatusBadGateway, "TIMELINE_UNAVAILABLE", "failed to save timeline view")
		return
	}
	var result entity.TimelineSummary
	if err := handle.Get(r.Context(), &result); err != nil {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) && appErr.Type() == entity.ErrTypeTimelineNotActive {
			writeStructuredError(w, http.StatusConflict, entity.ErrTypeTimelineNotActive, appErr.Message())
			return
		}
		writeStructuredError(w, http.StatusUnprocessableEntity, "INVALID_TIMELINE_VIEW", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetTranscriptView(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeStructuredError(
			w, http.StatusBadGateway, "TIMELINE_UNAVAILABLE", "failed to load timeline",
		)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		timeline.BuildTranscriptView(ref.ProjectID, state.Revision, state.Doc),
	)
}

type deleteTranscriptRangesRequest struct {
	ClientMutationID string                           `json:"clientMutationId"`
	TimelineID       string                           `json:"timelineId,omitempty"`
	BaseRevision     uint64                           `json:"baseRevision"`
	Ranges           []timeline.TranscriptDeleteRange `json:"ranges"`
	Mode             string                           `json:"mode"`
}

func (s *Server) handleDeleteTranscriptRanges(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	var req deleteTranscriptRangesRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(timeline.MaxDocBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := entity.ValidateClientMutationID(req.ClientMutationID); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	updateReq := entity.DeleteTranscriptRangesRequest{
		TimelineID:   req.TimelineID,
		BaseRevision: req.BaseRevision,
		Source:       "user",
		Mode:         req.Mode,
		Ranges:       req.Ranges,
	}
	if err := entity.ValidateDeleteTranscriptRangesRequest(updateReq); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	result, httpErr := s.deleteTranscriptRanges(
		r.Context(), ref.ProjectID, req.ClientMutationID, updateReq,
	)
	if httpErr != nil {
		httpErr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type timelineOpsRequest struct {
	// RequestID is the legacy name retained for old clients.
	RequestID string `json:"requestId,omitempty"`
	// ClientMutationID may only be reused for a byte-for-byte equivalent
	// network retry. A corrected request after 409/422 must use a new ID.
	ClientMutationID string               `json:"clientMutationId,omitempty"`
	TimelineID       string               `json:"timelineId,omitempty"`
	BaseRevision     uint64               `json:"baseRevision"`
	Operations       []timeline.Operation `json:"operations"`
}

type restoreTimelineRequest struct {
	ClientMutationID string       `json:"clientMutationId"`
	TimelineID       string       `json:"timelineId,omitempty"`
	BaseRevision     uint64       `json:"baseRevision"`
	Doc              timeline.Doc `json:"doc"`
}

func (s *Server) handleRestoreTimeline(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	var req restoreTimelineRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(timeline.MaxDocBytes+(64<<10))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if err := entity.ValidateClientMutationID(req.ClientMutationID); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if validationErr := timeline.ValidateDoc(req.Doc); validationErr != nil {
		writeStructuredError(w, http.StatusUnprocessableEntity, string(validationErr.Code), validationErr.Message)
		return
	}
	handle, err := s.Temporal.UpdateWorkflow(r.Context(), client.UpdateWorkflowOptions{
		WorkflowID: entity.TimelineWorkflowID(ref.ProjectID),
		UpdateName: entity.UpdateRestoreTimeline,
		UpdateID: entity.TimelineSequenceMutationUpdateID(
			ref.ProjectID, req.TimelineID, "user", req.ClientMutationID,
		),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args: []any{entity.RestoreTimelineRequest{
			TimelineID:   req.TimelineID,
			BaseRevision: req.BaseRevision,
			Source:       "user",
			Doc:          req.Doc,
		}},
	})
	if err != nil {
		writeStructuredError(w, http.StatusBadGateway, "TIMELINE_UNAVAILABLE", "failed to reach timeline workflow")
		return
	}
	var result entity.RestoreTimelineResult
	if err := handle.Get(r.Context(), &result); err != nil {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) && appErr.Type() == entity.ErrTypeRevisionConflict {
			var details entity.RevisionConflictDetails
			_ = appErr.Details(&details)
			writeJSON(w, http.StatusConflict, map[string]any{
				"code": entity.ErrTypeRevisionConflict, "latestRevision": details.LatestRevision,
				"error": "timeline changed before it could be restored",
			})
			return
		}
		if errors.As(err, &appErr) && appErr.Type() == entity.ErrTypeTimelineNotActive {
			writeStructuredError(w, http.StatusConflict, entity.ErrTypeTimelineNotActive, appErr.Message())
			return
		}
		if errors.As(err, &appErr) && appErr.Type() == entity.ErrTypeOpFailed {
			writeStructuredError(w, http.StatusUnprocessableEntity, entity.ErrTypeOpFailed, appErr.Message())
			return
		}
		writeStructuredError(w, http.StatusBadGateway, "TIMELINE_RESTORE_FAILED", "timeline restore failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTimelineOps(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	var req timelineOpsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(timeline.MaxDocBytes))).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	clientMutationID := req.ClientMutationID
	if clientMutationID == "" {
		clientMutationID = req.RequestID
	}
	if req.ClientMutationID != "" && req.RequestID != "" && req.ClientMutationID != req.RequestID {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST",
			"requestId and clientMutationId must match when both are supplied")
		return
	}
	if err := entity.ValidateClientMutationID(clientMutationID); err != nil {
		writeStructuredError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if len(req.Operations) == 0 || len(req.Operations) > timeline.MaxOpsPerBatch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("operations must contain 1..%d ops", timeline.MaxOpsPerBatch))
		return
	}
	res, herr := s.applyOpsToTimeline(
		r.Context(), ref.ProjectID, req.TimelineID, clientMutationID,
		"user", req.BaseRevision, req.Operations,
	)
	if herr != nil {
		herr.write(w)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// httpError 是 Update 错误到 HTTP 的映射结果。
type httpError struct {
	status  int
	payload map[string]any
}

func (e *httpError) write(w http.ResponseWriter) { writeJSON(w, e.status, e.payload) }

// applyOps 把一批 op 转发给 Timeline 实体 Workflow，并做错误映射。
func (s *Server) applyOps(ctx context.Context, projectID, clientMutationID, source string, baseRevision uint64, ops []timeline.Operation) (*entity.ApplyOpsResult, *httpError) {
	return s.applyOpsToTimeline(
		ctx, projectID, "", clientMutationID, source, baseRevision, ops,
	)
}

func (s *Server) applyOpsToTimeline(
	ctx context.Context,
	projectID string,
	timelineID string,
	clientMutationID string,
	source string,
	baseRevision uint64,
	ops []timeline.Operation,
) (*entity.ApplyOpsResult, *httpError) {
	request := entity.ApplyOpsRequest{
		TimelineID:   timelineID,
		BaseRevision: baseRevision,
		Source:       source,
		Operations:   ops,
	}
	if err := entity.ValidateApplyOpsRequest(request); err != nil {
		return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
			"code": "BAD_REQUEST", "message": err.Error(), "error": err.Error(),
		}}
	}
	handle, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   entity.TimelineWorkflowID(projectID),
		UpdateName:   entity.UpdateApplyOps,
		UpdateID:     entity.TimelineSequenceMutationUpdateID(projectID, timelineID, source, clientMutationID),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{request},
	})
	if err != nil {
		return nil, &httpError{http.StatusBadGateway, map[string]any{"error": "failed to reach timeline workflow"}}
	}
	var res entity.ApplyOpsResult
	if err := handle.Get(ctx, &res); err != nil {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) {
			switch appErr.Type() {
			case entity.ErrTypeRevisionConflict:
				var details entity.RevisionConflictDetails
				_ = appErr.Details(&details)
				message := fmt.Sprintf("baseRevision does not match latest revision %d", details.LatestRevision)
				return nil, &httpError{http.StatusConflict, map[string]any{
					"code":           entity.ErrTypeRevisionConflict,
					"latestRevision": details.LatestRevision,
					"message":        message,
					"error":          message,
				}}
			case entity.ErrTypeOpFailed:
				var oe timeline.OpError
				if derr := appErr.Details(&oe); derr == nil {
					return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
						"code": oe.Code, "opIndex": oe.Index, "op": oe.Op, "error": oe.Message,
					}}
				}
				return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
					"code": entity.ErrTypeOpFailed, "error": appErr.Message(),
				}}
			case entity.ErrTypeTimelineNotActive:
				return nil, &httpError{http.StatusConflict, map[string]any{
					"code": entity.ErrTypeTimelineNotActive, "error": appErr.Message(),
				}}
			}
		}
		return nil, &httpError{http.StatusBadGateway, map[string]any{"error": "timeline update failed"}}
	}
	return &res, nil
}

func (s *Server) deleteTranscriptRanges(
	ctx context.Context,
	projectID string,
	clientMutationID string,
	request entity.DeleteTranscriptRangesRequest,
) (*entity.DeleteTranscriptRangesResult, *httpError) {
	handle, err := s.Temporal.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID: entity.TimelineWorkflowID(projectID),
		UpdateName: entity.UpdateDeleteTranscript,
		UpdateID: entity.TimelineSequenceMutationUpdateID(
			projectID, request.TimelineID, request.Source, clientMutationID,
		),
		WaitForStage: client.WorkflowUpdateStageCompleted,
		Args:         []any{request},
	})
	if err != nil {
		return nil, &httpError{http.StatusBadGateway, map[string]any{
			"code": "TIMELINE_UNAVAILABLE", "error": "failed to reach timeline workflow",
		}}
	}
	var result entity.DeleteTranscriptRangesResult
	if err := handle.Get(ctx, &result); err != nil {
		var appErr *temporal.ApplicationError
		if errors.As(err, &appErr) {
			switch appErr.Type() {
			case entity.ErrTypeRevisionConflict:
				var details entity.RevisionConflictDetails
				_ = appErr.Details(&details)
				message := fmt.Sprintf(
					"baseRevision does not match latest revision %d",
					details.LatestRevision,
				)
				return nil, &httpError{http.StatusConflict, map[string]any{
					"code":           entity.ErrTypeRevisionConflict,
					"latestRevision": details.LatestRevision,
					"message":        message,
					"error":          message,
				}}
			case entity.ErrTypeTranscriptEdit:
				var domainErr timeline.TranscriptEditError
				if detailErr := appErr.Details(&domainErr); detailErr == nil {
					return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
						"code": domainErr.Code, "message": domainErr.Message, "error": domainErr.Message,
					}}
				}
				return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
					"code": entity.ErrTypeTranscriptEdit, "message": appErr.Message(), "error": appErr.Message(),
				}}
			case entity.ErrTypeOpFailed:
				var opErr timeline.OpError
				if detailErr := appErr.Details(&opErr); detailErr == nil {
					return nil, &httpError{http.StatusUnprocessableEntity, map[string]any{
						"code": string(opErr.Code), "message": opErr.Message, "error": opErr.Message,
					}}
				}
			case entity.ErrTypeTimelineNotActive:
				return nil, &httpError{http.StatusConflict, map[string]any{
					"code": entity.ErrTypeTimelineNotActive, "error": appErr.Message(),
				}}
			}
		}
		return nil, &httpError{http.StatusBadGateway, map[string]any{
			"code": "TIMELINE_UPDATE_FAILED", "error": "timeline update failed",
		}}
	}
	return &result, nil
}

// ---- 工具函数 ----

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeStructuredError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"code": code, "message": message, "error": message,
	})
}
