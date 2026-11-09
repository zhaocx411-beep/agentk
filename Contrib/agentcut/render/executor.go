package render

import (
	"fmt"
	"os"
	"strings"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"agentcut/timeline"
)

// ExecutorKind is persisted in Workflow input. Never derive it from process
// environment inside Workflow code: doing so would make replay depend on the
// worker that happens to pick up a task.
type ExecutorKind string

const (
	ExecutorLocal  ExecutorKind = "local"
	ExecutorAgentK ExecutorKind = "agentk"

	DefaultLocalProfile  = "ffmpeg-h264-aac-v1"
	DefaultAgentKProfile = "agentk-ffmpeg-v1"
	LegacyAgentKProfile  = "legacy-agentk-ffmpeg"
	RenderSchemaVersion  = "agentcut-render-v4"
)

// Selection is the startup-time render configuration copied into every new
// Workflow input and render key.
type Selection struct {
	Executor ExecutorKind `json:"executor"`
	Profile  string       `json:"profile"`
}

// SelectionFromEnv validates render configuration at process startup.
func SelectionFromEnv() (Selection, error) {
	return ParseSelection(os.Getenv("AGENTCUT_RENDER_EXECUTOR"), os.Getenv("AGENTCUT_RENDER_PROFILE"))
}

// ParseSelection is pure so worker and API startup use exactly the same rules.
func ParseSelection(rawExecutor, rawProfile string) (Selection, error) {
	kind := ExecutorKind(strings.ToLower(strings.TrimSpace(rawExecutor)))
	if kind == "" {
		kind = ExecutorLocal
	}
	switch kind {
	case ExecutorLocal, ExecutorAgentK:
	case "kess":
		return Selection{}, fmt.Errorf("AGENTCUT_RENDER_EXECUTOR=kess is unsupported; use local or agentk")
	default:
		return Selection{}, fmt.Errorf("AGENTCUT_RENDER_EXECUTOR must be local or agentk (got %q)", rawExecutor)
	}
	profile := strings.TrimSpace(rawProfile)
	if profile == "" {
		if kind == ExecutorLocal {
			profile = DefaultLocalProfile
		} else {
			profile = DefaultAgentKProfile
		}
	}
	if len(profile) > 128 {
		return Selection{}, fmt.Errorf("AGENTCUT_RENDER_PROFILE must be at most 128 bytes")
	}
	for _, r := range profile {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return Selection{}, fmt.Errorf("AGENTCUT_RENDER_PROFILE must match [A-Za-z0-9._-]+")
	}
	return Selection{Executor: kind, Profile: profile}, nil
}

// SelectForDoc resolves executor capability before a Workflow is started.
// Availability is supplied by the API process; no environment or I/O is read
// here so the chosen value can be persisted in the render key and Workflow
// input deterministically.
func SelectForDoc(configured Selection, localAvailable bool, doc timeline.Doc) (Selection, error) {
	if configured.Executor == ExecutorLocal {
		if !localAvailable {
			return Selection{}, &UnsupportedRenderFeatureError{
				Feature: "local executor storage is not configured",
			}
		}
		return configured, nil
	}
	if configured.Executor != ExecutorAgentK {
		return configured, nil
	}
	if err := rejectUnsupportedAgentKFeatures(doc); err != nil {
		if localAvailable {
			return Selection{Executor: ExecutorLocal, Profile: DefaultLocalProfile}, nil
		}
		return Selection{}, err
	}
	return configured, nil
}

// EffectiveSelection preserves replay compatibility for Workflow histories
// created before executor/profile fields existed. A missing executor always
// means the legacy Agent-K capability path, never the new local default.
func EffectiveSelection(in Input) Selection {
	if in.Executor == "" {
		return Selection{Executor: ExecutorAgentK, Profile: LegacyAgentKProfile}
	}
	profile := strings.TrimSpace(in.Profile)
	if profile == "" {
		if in.Executor == ExecutorLocal {
			profile = DefaultLocalProfile
		} else {
			profile = DefaultAgentKProfile
		}
	}
	return Selection{Executor: in.Executor, Profile: profile}
}

type ExecuteRequest struct {
	ProjectID       string
	RenderKey       string
	Call            Call
	Input           map[string]any
	Media           []timeline.Media
	Width           int
	Height          int
	FPS             int
	BackgroundColor string
	Final           bool
}

type ExecuteResult struct {
	Ref string
}

// RenderExecutor is the stable Workflow-side execution boundary.
type RenderExecutor interface {
	Execute(workflow.Context, ExecuteRequest) (ExecuteResult, error)
}

// LocalFFmpegExecutor schedules a single local Activity. It performs no I/O in
// Workflow code.
type LocalFFmpegExecutor struct{}

func (LocalFFmpegExecutor) Execute(ctx workflow.Context, req ExecuteRequest) (ExecuteResult, error) {
	ctx = workflow.WithActivityOptions(ctx, localRenderActivityOptions())
	var out LocalActivityOutput
	err := workflow.ExecuteActivity(ctx, LocalActivityName, LocalActivityInput{
		ProjectID:       req.ProjectID,
		RenderKey:       req.RenderKey,
		CallID:          req.Call.ID,
		Input:           req.Input,
		Media:           req.Media,
		Width:           req.Width,
		Height:          req.Height,
		FPS:             req.FPS,
		BackgroundColor: req.BackgroundColor,
		Final:           req.Final,
	}).Get(ctx, &out)
	return ExecuteResult{Ref: out.Key}, err
}

func localRenderActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        2 * time.Second,
			BackoffCoefficient:     2,
			MaximumInterval:        30 * time.Second,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: []string{"BLOB_NOT_CONFIGURED", "LOCAL_RENDER_INVALID", "RENDER_SOURCE_REJECTED"},
		},
	}
}

// AgentKFFmpegExecutor is the legacy remote capability adapter.
type AgentKFFmpegExecutor struct{}

func (AgentKFFmpegExecutor) Execute(ctx workflow.Context, req ExecuteRequest) (ExecuteResult, error) {
	var out ffmpegOutput
	if err := sdkcapability.Invoke(ctx, FfmpegCapabilityID, req.Input, &out); err != nil {
		return ExecuteResult{}, err
	}
	if len(out.Artifacts) == 0 || strings.TrimSpace(out.Artifacts[0].URI) == "" {
		return ExecuteResult{}, fmt.Errorf("call %s returned no artifact", req.Call.ID)
	}
	return ExecuteResult{Ref: out.Artifacts[0].URI}, nil
}

func executorFor(kind ExecutorKind) (RenderExecutor, error) {
	switch kind {
	case ExecutorLocal:
		return LocalFFmpegExecutor{}, nil
	case ExecutorAgentK:
		return AgentKFFmpegExecutor{}, nil
	default:
		return nil, fmt.Errorf("unsupported render executor %q", kind)
	}
}
