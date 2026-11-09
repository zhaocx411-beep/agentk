package comicdrama

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"studio/platform/core"
)

type DramaStatus string

const (
	// DramaRunning 两种模式通用：任务仍在推进。
	DramaRunning DramaStatus = "running"
	// DramaCompleted / DramaFailed / DramaCancelled pipeline 模式的终态。
	DramaCompleted DramaStatus = "completed"
	DramaFailed    DramaStatus = "failed"
	DramaCancelled DramaStatus = "cancelled"
	// DramaIdle / DramaError agent 模式的收敛与出错态（会话仍可继续对话）。
	DramaIdle  DramaStatus = "idle"
	DramaError DramaStatus = "error"
)

// Drama 是一个漫剧任务：一对一绑定一个 AgentK Session。
//
// pipeline 模式下还绑定一次 pipeline 执行（ExecID）：状态与资产清单来自
// run 快照，阶段进度由账本 invocation.progress 事件折叠（fold.go）。
// agent 模式下 ExecID 为空：状态由账本整体折叠成 DramaState（fold_agent.go）。
type Drama struct {
	ID          string
	SessionID   string
	ExecID      string
	Title       string
	Brief       string
	StylePrompt string
	Status      DramaStatus
	// Manifest pipeline 完成时的完整资产清单；运行中或 agent 模式为 nil。
	Manifest *core.DramaManifest
	Error    string
	// Progress 已折叠出的阶段进度（pipeline 模式）；LastEventID 是账本
	// 折叠游标（evt_ UUIDv7，字典序 == 时间序）。
	Progress    []ProgressStep
	LastEventID string
	// State agent 模式的账本折叠现场（pipeline 模式不使用）。
	State     DramaState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProgressStep 是 pipeline 一次进度汇报（invocation.progress 事件）的投影。
type ProgressStep struct {
	StepID  string    `json:"stepId"`
	Message string    `json:"message,omitempty"`
	At      time.Time `json:"at"`
}

type MemoryDramaStore struct {
	mu     sync.RWMutex
	dramas map[string]Drama
	order  []string
}

func NewMemoryDramaStore() *MemoryDramaStore {
	return &MemoryDramaStore{dramas: make(map[string]Drama)}
}

// RestoreAgentSessions rebuilds the volatile drama index from AgentK's durable
// session list after this service restarts. Only sessions created by the comic
// drama product are imported; their full state is folded from the ledger on
// the next refresh.
func RestoreAgentSessions(
	store *MemoryDramaStore,
	sessions []core.SessionSummary,
) int {
	restored := 0
	for _, session := range sessions {
		const titlePrefix = "漫剧: "
		if !strings.HasPrefix(session.Title, titlePrefix) ||
			!strings.HasPrefix(session.ID, "ses_") {
			continue
		}
		token := strings.TrimPrefix(session.ID, "ses_")
		if token == "" {
			continue
		}
		createdAt := session.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		state := NewDramaState()
		state.Converged = false
		if err := store.Create(Drama{
			ID:        "drm_" + token,
			SessionID: session.ID,
			Title:     strings.TrimSpace(strings.TrimPrefix(session.Title, titlePrefix)),
			Status:    DramaRunning,
			State:     state,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}); err == nil {
			restored++
		}
	}
	return restored
}

func (s *MemoryDramaStore) Create(drama Drama) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.dramas[drama.ID]; exists {
		return fmt.Errorf("drama %q already exists", drama.ID)
	}
	s.dramas[drama.ID] = cloneDrama(drama)
	s.order = append(s.order, drama.ID)
	return nil
}

func (s *MemoryDramaStore) Get(id string) (Drama, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drama, ok := s.dramas[id]
	return cloneDrama(drama), ok
}

func (s *MemoryDramaStore) List() []Drama {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dramas := make([]Drama, 0, len(s.order))
	for index := len(s.order) - 1; index >= 0; index-- {
		dramas = append(dramas, cloneDrama(s.dramas[s.order[index]]))
	}
	return dramas
}

// UpdateSnapshot 保存一次刷新后的现场。以 LastEventID 单调为准：并发刷新
// 时旧的进度游标不能覆盖新的；状态与清单直接取最新值（快照端点幂等）。
func (s *MemoryDramaStore) UpdateSnapshot(
	id string,
	status DramaStatus,
	manifest *core.DramaManifest,
	errMessage string,
	progress []ProgressStep,
	lastEventID string,
	updatedAt time.Time,
) (Drama, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	drama, ok := s.dramas[id]
	if !ok {
		return Drama{}, fmt.Errorf("drama %q not found", id)
	}
	drama.Status = status
	drama.Manifest = manifest
	drama.Error = errMessage
	if lastEventID >= drama.LastEventID {
		drama.LastEventID = lastEventID
		drama.Progress = progress
	}
	drama.UpdatedAt = updatedAt
	s.dramas[id] = drama
	return cloneDrama(drama), nil
}

// UpdateState 保存 agent 模式一次折叠后的现场。以 LastEventID 单调为准：
// 并发刷新时旧现场不能覆盖新现场。
func (s *MemoryDramaStore) UpdateState(
	id string,
	state DramaState,
	updatedAt time.Time,
) (Drama, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	drama, ok := s.dramas[id]
	if !ok {
		return Drama{}, fmt.Errorf("drama %q not found", id)
	}
	if state.LastEventID < drama.State.LastEventID {
		return cloneDrama(drama), nil
	}
	drama.State = state.Clone()
	drama.Status = state.Status()
	drama.UpdatedAt = updatedAt
	s.dramas[id] = drama
	return cloneDrama(drama), nil
}

func cloneDrama(drama Drama) Drama {
	if drama.Progress != nil {
		progress := make([]ProgressStep, len(drama.Progress))
		copy(progress, drama.Progress)
		drama.Progress = progress
	}
	if drama.Manifest != nil {
		manifest := *drama.Manifest
		manifest.Characters = append([]core.CharacterAsset(nil), drama.Manifest.Characters...)
		manifest.Voices = append([]core.VoiceAsset(nil), drama.Manifest.Voices...)
		manifest.Episodes = append([]core.EpisodeAsset(nil), drama.Manifest.Episodes...)
		drama.Manifest = &manifest
	}
	if drama.State.OpenCalls != nil {
		drama.State = drama.State.Clone()
	}
	return drama
}
