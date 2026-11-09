package capability

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
)

// ManifestProtocolVersion 是 capability discovery Manifest 的 wire 版本。
// V1 只承载实现契约与 Tool 消费策略，不承载在线状态或审计信息。
const ManifestProtocolVersion = 1

// CapabilityKind 是 capability 的 Temporal 实现种类。
type CapabilityKind string

const (
	CapabilityKindWorkflow CapabilityKind = "workflow"
	CapabilityKindActivity CapabilityKind = "activity"
)

// ToolMode is retained in the JSON discovery wire until discovery moves to protobuf.
type ToolMode string

const (
	ToolModeBlocking ToolMode = "blocking"
)

// ManifestV1 是一个 task queue 上所有已发布 capability 的发现快照。
// TaskQueue 由 SDK Worker 注入，Kind 由 PublishWorkflow/PublishActivity 注入。
type ManifestV1 struct {
	ProtocolVersion int             `json:"protocolVersion"`
	TaskQueue       string          `json:"taskQueue"`
	LaneID          string          `json:"laneId,omitempty"`
	Capabilities    []CapabilityDef `json:"capabilities"`
}

// CapabilityDef 是 Manifest 中的 capability wire 结构。
type CapabilityDef struct {
	ID             string          `json:"id"`
	Kind           CapabilityKind  `json:"kind"`
	TemporalType   string          `json:"temporalType"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"inputSchema"`
	OutputSchema   json.RawMessage `json:"outputSchema"`
	TimeoutSeconds int64           `json:"timeoutSeconds"`
	Tool           *ToolDecl       `json:"tool,omitempty"`
}

// ToolDecl 声明 capability 注册为 LLM Tool。Description 为空时继承 capability。
type ToolDecl struct {
	Description string   `json:"description"`
	Mode        ToolMode `json:"mode"`
	MaxAttempts int32    `json:"maxAttempts,omitempty"`
}

const maxCapabilityTimeoutSeconds int64 = 24 * 60 * 60

var capabilityIDPattern = regexp.MustCompile(`^(agentk\.[a-z][a-z0-9_]*|contrib\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*)$`)

// ValidateID 校验核心或 contributor 能力的全局标识。
func ValidateID(id string) error {
	if len(id) > 128 || !capabilityIDPattern.MatchString(id) {
		return fmt.Errorf(
			"id %q must match agentk.<name> or contrib.<owner>.<name>, each segment must match [a-z][a-z0-9_]*, and the total length must be at most 128 characters",
			id,
		)
	}
	return nil
}

// Normalize 校验 Manifest，深拷贝内嵌值并返回稳定排序的规范化快照。
func (m ManifestV1) Normalize() (ManifestV1, error) {
	return normalizeManifest(m)
}

// Validate 校验 Manifest 并确认其规范化结果合法。
func (m ManifestV1) Validate() error {
	_, err := m.Normalize()
	return err
}

func normalizeManifest(m ManifestV1) (ManifestV1, error) {
	if m.ProtocolVersion != ManifestProtocolVersion {
		return ManifestV1{}, fmt.Errorf("protocolVersion must be %d", ManifestProtocolVersion)
	}
	if strings.TrimSpace(m.TaskQueue) == "" {
		return ManifestV1{}, fmt.Errorf("taskQueue is required")
	}
	if m.LaneID != "" {
		if err := ksrpc.ValidateLane(m.LaneID); err != nil {
			return ManifestV1{}, fmt.Errorf("laneId: %w", err)
		}
	}

	out := ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       m.TaskQueue,
		LaneID:          m.LaneID,
		Capabilities:    make([]CapabilityDef, 0, len(m.Capabilities)),
	}
	capabilityIDs := make(map[string]struct{}, len(m.Capabilities))

	for i, def := range m.Capabilities {
		normalized, err := normalizeCapability(def)
		if err != nil {
			return ManifestV1{}, fmt.Errorf("capabilities[%d]: %w", i, err)
		}
		if _, exists := capabilityIDs[normalized.ID]; exists {
			return ManifestV1{}, fmt.Errorf("duplicate capability id %q", normalized.ID)
		}
		capabilityIDs[normalized.ID] = struct{}{}
		out.Capabilities = append(out.Capabilities, normalized)
	}

	slices.SortFunc(out.Capabilities, func(a, b CapabilityDef) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func normalizeCapability(def CapabilityDef) (CapabilityDef, error) {
	if err := ValidateID(def.ID); err != nil {
		return CapabilityDef{}, err
	}
	switch def.Kind {
	case CapabilityKindWorkflow, CapabilityKindActivity:
	default:
		return CapabilityDef{}, fmt.Errorf("unknown kind %q", def.Kind)
	}
	if strings.TrimSpace(def.TemporalType) == "" {
		return CapabilityDef{}, fmt.Errorf("temporalType is required")
	}
	if strings.TrimSpace(def.Description) == "" {
		return CapabilityDef{}, fmt.Errorf("description is required")
	}
	if def.TimeoutSeconds <= 0 || def.TimeoutSeconds > maxCapabilityTimeoutSeconds {
		return CapabilityDef{}, fmt.Errorf("timeoutSeconds must be in (0, %d]", maxCapabilityTimeoutSeconds)
	}

	inputSchema, err := normalizeSchema(def.InputSchema, true)
	if err != nil {
		return CapabilityDef{}, fmt.Errorf("inputSchema: %w", err)
	}
	outputSchema, err := normalizeSchema(def.OutputSchema, false)
	if err != nil {
		return CapabilityDef{}, fmt.Errorf("outputSchema: %w", err)
	}

	out := CapabilityDef{
		ID:             def.ID,
		Kind:           def.Kind,
		TemporalType:   def.TemporalType,
		Description:    def.Description,
		InputSchema:    inputSchema,
		OutputSchema:   outputSchema,
		TimeoutSeconds: def.TimeoutSeconds,
	}
	if def.Tool != nil {
		tool, err := normalizeTool(*def.Tool, def.Description)
		if err != nil {
			return CapabilityDef{}, fmt.Errorf("tool: %w", err)
		}
		out.Tool = &tool
	}
	return out, nil
}

func normalizeTool(decl ToolDecl, fallbackDescription string) (ToolDecl, error) {
	if decl.MaxAttempts < 0 {
		return ToolDecl{}, fmt.Errorf("maxAttempts must be non-negative")
	}
	switch decl.Mode {
	case ToolModeBlocking:
	default:
		return ToolDecl{}, fmt.Errorf("unknown mode %q", decl.Mode)
	}
	if strings.TrimSpace(decl.Description) == "" {
		decl.Description = fallbackDescription
	}
	return decl, nil
}
