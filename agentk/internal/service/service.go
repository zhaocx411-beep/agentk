// Package service 实现与传输协议无关的 AgentK 应用操作。HTTP 和 Connect handler
// 只负责 wire 映射；业务编排、租户隔离和审计统一在本包完成，避免 transport
// 各自实现时遗漏安全边界或产生行为差异。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"agentk/internal/audit"
	"agentk/internal/authz"
	"agentk/internal/capcatalog"
	"agentk/internal/event"
	"agentk/internal/execution"
	"agentk/internal/fold"
	"agentk/internal/pipeline"
	"agentk/internal/types"
)

// Store 是 service 对 Session 持久化的最小依赖。
type Store interface {
	CreateSession(ctx context.Context, tenantID, title string, profile []byte) (types.Session, error)
	GetSessionByPublicID(ctx context.Context, tenantID, publicID string) (types.Session, error)
	ListSessions(ctx context.Context, tenantID string, beforeID int64, limit int) ([]types.Session, error)
	LoadSessionEvents(ctx context.Context, sessionID int64) ([]event.Event, error)
}

// AgentLoopRunner 启动 Session 的 Agent Loop；Pipeline 是平级的显式执行入口。
type AgentLoopRunner interface {
	StartMessage(ctx context.Context, sess types.Session, text string) (types.ExecutionID, error)
	StartAnswer(ctx context.Context, sess types.Session, answer event.AskUserResponded) (types.ExecutionID, error)
}

type ExecutionCanceller interface {
	Cancel(ctx context.Context, sess types.Session) error
}

// PipelineRunner 查询和启动 Session Profile 中声明的 Pipeline。
type PipelineRunner interface {
	List(ctx context.Context, sess types.Session) ([]pipeline.ListItem, error)
	Start(ctx context.Context, sess types.Session, name string, args json.RawMessage) (types.Execution, error)
}

// CapabilityDirectory 提供只读 capability catalog。
type CapabilityDirectory interface {
	ListCapabilities(ctx context.Context) ([]capcatalog.ResolvedCapability, error)
}

// SessionProfileRuntime 固化并校验 Session Profile。
type SessionProfileRuntime interface {
	MaterializeSessionProfile(requestJSON json.RawMessage) (json.RawMessage, error)
	ValidateSessionProfile(profileJSON []byte) error
}

// Config 是 Service 的依赖；Pipelines 和 Capabilities 可以禁用。
type Config struct {
	Store          Store
	AgentLoop      AgentLoopRunner
	Executions     ExecutionCanceller
	ProfileRuntime SessionProfileRuntime
	Pipelines      PipelineRunner
	Capabilities   CapabilityDirectory
}

// Service 实现可由不同 transport 调用的应用操作。
type Service struct {
	store        Store
	agentLoop    AgentLoopRunner
	executions   ExecutionCanceller
	runtime      SessionProfileRuntime
	pipelines    PipelineRunner
	capabilities CapabilityDirectory
}

func New(cfg Config) *Service {
	if cfg.Store == nil || cfg.AgentLoop == nil || cfg.Executions == nil || cfg.ProfileRuntime == nil {
		panic("service: store, agent loop, execution canceller and profile runtime are required")
	}
	return &Service{
		store:        cfg.Store,
		agentLoop:    cfg.AgentLoop,
		executions:   cfg.Executions,
		runtime:      cfg.ProfileRuntime,
		pipelines:    cfg.Pipelines,
		capabilities: cfg.Capabilities,
	}
}

var (
	ErrInvalidArgument       = errors.New("invalid argument")
	ErrConflict              = errors.New("conflict")
	ErrFeatureDisabled       = errors.New("feature disabled")
	ErrDependencyUnavailable = errors.New("dependency unavailable")
	ErrOperationNotFound     = errors.New("operation resource not found")
)

type classifiedError struct {
	class error
	cause error
	msg   string
}

func (e *classifiedError) Error() string { return e.msg }

func (e *classifiedError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.class}
	}
	return []error{e.class, e.cause}
}

func classify(class, cause error, msg string) error {
	return &classifiedError{class: class, cause: cause, msg: msg}
}

type SessionPage struct {
	Sessions []types.Session
	HasMore  bool
}

// ResolveSession 只在 principal 所属 tenant 内解析 Session ID。
func (s *Service) ResolveSession(
	ctx context.Context, principal authz.Principal, sessionID string,
) (types.Session, error) {
	return s.store.GetSessionByPublicID(ctx, principal.TenantID, sessionID)
}

// CreateSession 校验并固化调用方提供的完整 Profile；服务端不补充默认 Profile。
func (s *Service) CreateSession(
	ctx context.Context,
	principal authz.Principal,
	title string,
	profile json.RawMessage,
) (types.Session, error) {
	profileJSON, err := s.runtime.MaterializeSessionProfile(profile)
	if err != nil {
		return types.Session{}, classify(
			ErrInvalidArgument, err, fmt.Sprintf("invalid profile: %v", err))
	}
	sess, err := s.store.CreateSession(ctx, principal.TenantID, title, profileJSON)
	if err != nil {
		return types.Session{}, err
	}
	audit.Record(authz.WithPrincipal(ctx, principal), "session.create", sess.PublicID)
	return sess, nil
}

// ListSessions 使用 tenant-scoped Session public ID 作为 keyset cursor，并多取一项
// 计算 HasMore。其他 tenant 的 cursor 与未知 Session 一样返回 not found。
func (s *Service) ListSessions(
	ctx context.Context,
	principal authz.Principal,
	beforeSessionID string,
	limit int,
) (SessionPage, error) {
	if limit <= 0 {
		return SessionPage{}, classify(ErrInvalidArgument, nil, "limit must be a positive integer")
	}
	var beforeID int64
	if beforeSessionID != "" {
		cursor, err := s.ResolveSession(ctx, principal, beforeSessionID)
		if err != nil {
			return SessionPage{}, err
		}
		beforeID = cursor.ID
	}
	sessions, err := s.store.ListSessions(ctx, principal.TenantID, beforeID, limit+1)
	if err != nil {
		return SessionPage{}, err
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	return SessionPage{Sessions: sessions, HasMore: hasMore}, nil
}

// SubmitMessage 为用户输入启动一次 Agent Loop。已有活跃 Execution 时
// 返回冲突；排队、重试或丢弃由调用方决定。
func (s *Service) SubmitMessage(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
	text string,
) (types.ExecutionID, error) {
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", classify(ErrInvalidArgument, nil, "text is required")
	}
	if err := s.runtime.ValidateSessionProfile(sess.Profile); err != nil {
		return "", classify(ErrConflict, err,
			fmt.Sprintf("session profile no longer valid: %v", err))
	}
	executionID, err := s.agentLoop.StartMessage(ctx, sess, text)
	if err != nil {
		if errors.Is(err, types.ErrExecutionActive) {
			return "", classify(ErrConflict, err, "session already has an active execution")
		}
		return "", classify(ErrDependencyUnavailable, err,
			fmt.Sprintf("post message to session %s: %v", sess.PublicID, err))
	}
	audit.Record(authz.WithPrincipal(ctx, principal), "session.message", sess.PublicID)
	return executionID, nil
}

// ListEvents 返回 Session 的完整事件日志。
func (s *Service) ListEvents(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
) ([]event.Event, error) {
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return nil, err
	}
	return s.store.LoadSessionEvents(ctx, sess.ID)
}

// SubmitAnswers 只接受当前仍待回答的 ask_user 调用，规范化答案后启动
// 一次新的 Agent Loop。回答事件由 Workflow 的首个 Activity 写入。
func (s *Service) SubmitAnswers(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
	toolCallID string,
	answers []event.Answer,
) (types.ExecutionID, error) {
	if toolCallID == "" {
		return "", classify(ErrInvalidArgument, nil, "toolCallId is required")
	}
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return "", err
	}
	events, err := s.store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		return "", err
	}
	ask, ok := findPendingAsk(fold.PendingAsks(events), event.ToolCallID(toolCallID))
	if !ok {
		return "", classify(ErrConflict, nil,
			"this question is no longer awaiting an answer")
	}
	normalized, err := normalizeAnswers(ask, answers)
	if err != nil {
		return "", classify(ErrInvalidArgument, err, err.Error())
	}
	executionID, err := s.agentLoop.StartAnswer(ctx, sess, event.AskUserResponded{
		ToolCallID: ask.ToolCallID, Answers: normalized,
	})
	if err != nil {
		if errors.Is(err, types.ErrExecutionActive) {
			return "", classify(ErrConflict, err, "session already has an active execution")
		}
		return "", classify(ErrDependencyUnavailable, err,
			fmt.Sprintf("answer session %s: %v", sess.PublicID, err))
	}
	audit.Record(authz.WithPrincipal(ctx, principal), "session.answer", sess.PublicID)
	return executionID, nil
}

// CancelSession 取消 Session 当前的 Agent Loop 或 Pipeline Execution。
func (s *Service) CancelSession(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
) error {
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return err
	}
	if err := s.executions.Cancel(ctx, sess); err != nil {
		if errors.Is(err, types.ErrExecutionIdle) {
			return classify(ErrConflict, err, "session has no active execution")
		}
		return classify(ErrDependencyUnavailable, err,
			fmt.Sprintf("cancel session %s: %v", sess.PublicID, err))
	}
	audit.Record(authz.WithPrincipal(ctx, principal), "session.cancel", sess.PublicID)
	return nil
}

// ListPipelines 返回当前仍可从 capability catalog 解析的 Profile Pipeline。
func (s *Service) ListPipelines(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
) ([]pipeline.ListItem, error) {
	if s.pipelines == nil {
		return nil, classify(ErrFeatureDisabled, nil,
			"pipeline execution is not enabled on this instance")
	}
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return nil, err
	}
	items, err := s.pipelines.List(ctx, sess)
	if err != nil {
		return nil, classify(ErrDependencyUnavailable, err, err.Error())
	}
	return items, nil
}

// ListExecutions 从 Session 事件日志投影全部 Execution。
func (s *Service) ListExecutions(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
) ([]types.Execution, error) {
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return nil, err
	}
	events, err := s.store.LoadSessionEvents(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	return execution.Project(sess, events), nil
}

// StartPipeline 解析 Profile 声明，并在 Temporal 接受后立即返回执行快照。
func (s *Service) StartPipeline(
	ctx context.Context,
	principal authz.Principal,
	sessionID string,
	name string,
	args json.RawMessage,
) (types.Execution, error) {
	if s.pipelines == nil {
		return types.Execution{}, classify(ErrFeatureDisabled, nil,
			"pipeline execution is not enabled on this instance")
	}
	if name == "" {
		return types.Execution{}, classify(ErrInvalidArgument, nil, "pipeline is required")
	}
	sess, err := s.ResolveSession(ctx, principal, sessionID)
	if err != nil {
		return types.Execution{}, err
	}
	exec, err := s.pipelines.Start(ctx, sess, name, args)
	if err != nil {
		switch {
		case errors.Is(err, pipeline.ErrInvalidArguments):
			return types.Execution{}, classify(ErrInvalidArgument, err, err.Error())
		case errors.Is(err, types.ErrNotFound):
			return types.Execution{}, classify(ErrOperationNotFound, err, err.Error())
		case errors.Is(err, types.ErrExecutionActive):
			return types.Execution{}, classify(ErrConflict, err, "session already has an active execution")
		case errors.Is(err, pipeline.ErrUnavailable):
			return types.Execution{}, classify(ErrDependencyUnavailable, err, err.Error())
		default:
			return types.Execution{}, classify(ErrDependencyUnavailable, err,
				fmt.Sprintf("start pipeline %s for session %s: %v", name, sess.PublicID, err))
		}
	}
	audit.Record(authz.WithPrincipal(ctx, principal), "pipeline.start", string(exec.ID))
	return exec, nil
}

// ListCapabilities 返回部署级 capability catalog，不按 tenant 过滤。
func (s *Service) ListCapabilities(
	ctx context.Context,
	_ authz.Principal,
) ([]capcatalog.ResolvedCapability, error) {
	if s.capabilities == nil {
		return nil, classify(ErrFeatureDisabled, nil,
			"capability catalog is not enabled on this instance")
	}
	items, err := s.capabilities.ListCapabilities(ctx)
	if err != nil {
		return nil, classify(ErrDependencyUnavailable, err,
			fmt.Sprintf("list capabilities: %v", err))
	}
	return items, nil
}

func findPendingAsk(pending []fold.PendingAsk, id event.ToolCallID) (fold.PendingAsk, bool) {
	for _, ask := range pending {
		if ask.ToolCallID == id {
			return ask, true
		}
	}
	return fold.PendingAsk{}, false
}

func normalizeAnswers(ask fold.PendingAsk, answers []event.Answer) ([]event.Answer, error) {
	if len(answers) == 0 {
		return nil, errors.New("answers is required")
	}
	byID := make(map[string]event.Question, len(ask.Questions))
	for _, question := range ask.Questions {
		byID[question.ID] = question
	}
	out := make([]event.Answer, 0, len(answers))
	for _, answer := range answers {
		question, ok := byID[answer.QuestionID]
		if !ok {
			return nil, fmt.Errorf("unknown questionId %q", answer.QuestionID)
		}
		if len(answer.Selected) == 0 {
			return nil, fmt.Errorf("question %q: selected is required", answer.QuestionID)
		}
		if !question.MultiSelect && len(answer.Selected) > 1 {
			return nil, fmt.Errorf("question %q accepts a single answer", answer.QuestionID)
		}
		out = append(out, event.Answer{
			QuestionID: question.ID,
			Question:   question.Question,
			Selected:   answer.Selected,
		})
	}
	return out, nil
}
