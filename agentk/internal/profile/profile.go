// Package profile 定义并解析创建 Session 时提供的 Profile JSON。
// Profile 是调用方完整提供的行为定义；服务端没有默认 Profile。它只按名称引用模型
// 目录，不包含凭证或环境变量引用，因此可以持久化和回显。
package profile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	"agentk/internal/agent"
	"agentk/internal/capcatalog"
	"agentk/internal/modelcatalog"
	"agentk/internal/modelcontract"
	"agentk/internal/pipeline"
	"agentk/internal/tool"
)

// Document 是 Profile JSON 的顶层结构，版本由规范化内容计算。
type Document struct {
	// DefaultModel 是必填的 chat 模型目录条目名。
	DefaultModel string `json:"defaultModel,omitempty"`
	// System 是主 Agent 的 system prompt。
	System string `json:"system"`
	// Tools 按提供给模型的顺序排列。
	Tools []ToolEntry `json:"tools"`
	// ToolDefaults 是按工具名配置的顶层默认参数，模型参数优先。
	ToolDefaults map[string]map[string]any `json:"toolDefaults,omitempty"`
	// Pipelines 是 Session 暴露的 Pipeline 引用和名称。
	Pipelines []pipeline.ProfileEntry `json:"pipelines,omitempty"`
	// MaxToolRounds 为零时使用 agent.DefaultMaxToolRounds；新的用户输入会重置计数。
	MaxToolRounds int `json:"maxToolRounds,omitempty"`
}

var canonicalToolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

// ToolEntry 选择一个本地工具，或以 capability ID 选择一个目录能力。
type ToolEntry struct {
	Capability   string `json:"capability,omitempty"`
	Ref          string `json:"ref,omitempty"` // 兼容旧 Profile，解析后清空。
	Name         string `json:"name"`
	Instructions string `json:"instructions,omitempty"`
}

func (e ToolEntry) Normalize() (ToolEntry, error) {
	if e.Capability != "" && e.Ref != "" {
		return e, fmt.Errorf("capability and ref cannot both be set")
	}
	if e.Ref != "" {
		if !strings.HasPrefix(e.Ref, "tool:") {
			return e, fmt.Errorf("legacy ref must start with %q", "tool:")
		}
		e.Capability = strings.TrimPrefix(e.Ref, "tool:")
		e.Ref = ""
	}
	return e, e.Validate()
}

func (e ToolEntry) Validate() error {
	if !canonicalToolNamePattern.MatchString(e.Name) {
		return fmt.Errorf("name must match %s", canonicalToolNamePattern)
	}
	if e.Capability == "" {
		return nil
	}
	if err := sdkcapability.ValidateID(e.Capability); err != nil {
		return fmt.Errorf("invalid capability %q", e.Capability)
	}
	return nil
}

func withInstructions(spec tool.Spec, instructions string) tool.Spec {
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return spec
	}
	if strings.TrimSpace(spec.Description) == "" {
		spec.Description = instructions
	} else {
		spec.Description += "\n\n" + instructions
	}
	return spec
}

// Built 是已校验并规范化的 Profile。Pipeline 在线状态不固化在 Profile 中，
// List/Start 时再从 capability catalog 解析。
type Built struct {
	Version string
	// Raw 是规范化且不含凭证的 JSON。
	Raw          []byte
	Doc          Document
	DefaultModel string
	System       string
	// ToolsByName 包含本地工具和 Session 声明工具。
	ToolsByName map[string]tool.Spec
	// Enabled 保留提供给模型的工具顺序。
	Enabled []ToolEntry
}

// Parse 严格校验并规范化不受信任的 Profile JSON。创建和读取已持久化 Profile
// 使用同一套规则，不存在权限更高的部署者格式。
func Parse(raw []byte, local []tool.Spec) (*Built, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("parse document: profile must be a JSON object")
	}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse document: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse document: multiple JSON values")
		}
		return nil, fmt.Errorf("parse document: %w", err)
	}
	defaultModel := doc.DefaultModel
	if defaultModel == "" {
		return nil, fmt.Errorf("defaultModel is required")
	}
	def, ok := modelcontract.Lookup(defaultModel)
	if !ok || def.Family != modelcontract.FamilyChat {
		return nil, fmt.Errorf("defaultModel %q is not a supported chat model", defaultModel)
	}
	if doc.MaxToolRounds < 0 {
		return nil, fmt.Errorf("maxToolRounds must be >= 0")
	}

	catalog := make(map[string]tool.Spec, len(local)+1)
	for _, s := range local {
		if _, dup := catalog[s.Name]; dup {
			return nil, fmt.Errorf("duplicate local tool name %q", s.Name)
		}
		catalog[s.Name] = s
	}
	seen := make(map[string]ToolEntry, len(doc.Tools))
	for i := range doc.Tools {
		entry, err := doc.Tools[i].Normalize()
		if err != nil {
			return nil, fmt.Errorf("tools %q: %w", entry.Name, err)
		}
		doc.Tools[i] = entry
		if entry.Capability == "" {
			spec, ok := catalog[entry.Name]
			if !ok {
				return nil, fmt.Errorf("tools: unknown tool %q", entry.Name)
			}
			catalog[entry.Name] = withInstructions(spec, entry.Instructions)
		}
		if _, dup := seen[entry.Name]; dup {
			return nil, fmt.Errorf("tools: duplicate tool %q", entry.Name)
		}
		seen[entry.Name] = entry
	}
	defaultNames := make([]string, 0, len(doc.ToolDefaults))
	for name := range doc.ToolDefaults {
		defaultNames = append(defaultNames, name)
	}
	sort.Strings(defaultNames)
	for _, name := range defaultNames {
		entry, enabled := seen[name]
		if !enabled {
			if _, ok := catalog[name]; !ok {
				return nil, fmt.Errorf("toolDefaults: unknown tool %q", name)
			}
			return nil, fmt.Errorf("toolDefaults: tool %q is not enabled", name)
		}
		if entry.Capability != "" {
			continue
		}
		configured, err := tool.WithDefaults(catalog[name], doc.ToolDefaults[name])
		if err != nil {
			return nil, fmt.Errorf("toolDefaults: %w", err)
		}
		catalog[name] = configured
	}

	// 固定目录选择，避免 Session 行为随运行时默认值变化。
	doc.DefaultModel = defaultModel
	if doc.Tools == nil {
		doc.Tools = []ToolEntry{}
	}
	// Session 创建时不要求 Pipeline Worker 已 announce。
	seenPipeline := make(map[string]bool, len(doc.Pipelines))
	for i := range doc.Pipelines {
		entry, err := doc.Pipelines[i].Normalize()
		if err != nil {
			return nil, fmt.Errorf("pipelines %q: %w", entry.Name, err)
		}
		doc.Pipelines[i] = entry
		if seenPipeline[entry.Name] {
			return nil, fmt.Errorf("pipelines: duplicate pipeline %q", entry.Name)
		}
		seenPipeline[entry.Name] = true
	}

	canonical, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical document: %w", err)
	}
	sum := sha256.Sum256(canonical)
	version := hex.EncodeToString(sum[:])[:12]

	return &Built{
		Version:      version,
		Raw:          canonical,
		Doc:          doc,
		DefaultModel: defaultModel,
		System:       doc.System,
		ToolsByName:  catalog,
		Enabled:      doc.Tools,
	}, nil
}

// Runtime 使用当前模型和 capability 目录校验 Session Profile。
// 它不生成默认文档；创建 Session 必须由调用方提供完整 Profile。
type Runtime struct {
	cat     *modelcatalog.Active
	catalog capcatalog.Catalog
	local   []tool.Spec
}

func NewRuntime(
	cat *modelcatalog.Active,
	catalog capcatalog.Catalog,
	local []tool.Spec,
) *Runtime {
	return &Runtime{cat: cat, catalog: catalog, local: local}
}

// MaterializeSessionProfile 校验必填 Profile，并返回规范 JSON。
// 本地工具和核心 capability 在创建时校验；contrib capability 只校验标识，不要求
// Worker 已经 announce。
func (r *Runtime) MaterializeSessionProfile(requestJSON json.RawMessage) (json.RawMessage, error) {
	raw := bytes.TrimSpace(requestJSON)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("session profile is required")
	}
	b, err := r.parse(raw)
	if err != nil {
		return nil, err
	}
	if err := r.validateCoreCapabilities(context.Background(), b); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), b.Raw...), nil
}

// ValidateSessionProfile 复核固定 Profile 引用的模型仍然可用。
// 动态工具短暂离线不应阻止已有 Session 接收消息，因此不检查其在线状态。
func (r *Runtime) ValidateSessionProfile(profileJSON []byte) error {
	b, err := r.parse(profileJSON)
	if err != nil {
		return err
	}
	return r.validateCoreCapabilities(context.Background(), b)
}

// Resolve 将持久化的 Session Profile 解析为执行配置。对话模型和工具集合由 Profile
// 固定；能力工具使用的具体模型由 tool call 参数选择，不属于 Profile。
func (r *Runtime) Resolve(ctx context.Context, profileJSON []byte) (agent.Resolved, error) {
	b, err := r.parse(profileJSON)
	if err != nil {
		return agent.Resolved{}, err
	}
	models := r.cat.Current()
	if models == nil {
		return agent.Resolved{}, fmt.Errorf("model catalog is not available")
	}
	return b.resolve(ctx, models, r.catalog)
}

func (r *Runtime) parse(profileJSON []byte) (*Built, error) {
	if len(bytes.TrimSpace(profileJSON)) == 0 {
		return nil, fmt.Errorf("session profile is required")
	}
	b, err := Parse(profileJSON, r.local)
	if err != nil {
		return nil, fmt.Errorf("session profile: %w", err)
	}
	return b, nil
}

func (b *Built) resolve(
	ctx context.Context,
	models *modelcatalog.Catalog,
	catalog capcatalog.Catalog,
) (agent.Resolved, error) {
	entry, ok := models.Chat[b.DefaultModel]
	if !ok {
		return agent.Resolved{}, fmt.Errorf("profile %s: model %q not available", b.Version, b.DefaultModel)
	}
	specs := make([]tool.Spec, 0, len(b.Enabled))
	for _, toolEntry := range b.Enabled {
		if toolEntry.Capability == "" {
			specs = append(specs, b.ToolsByName[toolEntry.Name])
			continue
		}
		if err := ctx.Err(); err != nil {
			return agent.Resolved{}, err
		}
		if catalog == nil {
			return agent.Resolved{}, fmt.Errorf(
				"profile %s: capability catalog is required for tool capability %q",
				b.Version, toolEntry.Capability)
		}
		resolved, err := catalog.ResolveTool(ctx, toolEntry.Capability)
		if err != nil {
			if ctx.Err() != nil {
				return agent.Resolved{}, ctx.Err()
			}
			return agent.Resolved{}, fmt.Errorf(
				"profile %s: resolve tool capability %q: %w",
				b.Version, toolEntry.Capability, err)
		}
		spec, err := specFromResolved(toolEntry, resolved)
		if err == nil {
			if defaults, ok := b.Doc.ToolDefaults[toolEntry.Name]; ok {
				spec, err = tool.WithDefaults(spec, defaults)
			}
		}
		if err != nil {
			return agent.Resolved{}, fmt.Errorf(
				"profile %s: build tool capability %q: %w",
				b.Version, toolEntry.Capability, err)
		}
		specs = append(specs, spec)
	}
	registry, err := tool.NewRegistry(specs...)
	if err != nil {
		return agent.Resolved{}, fmt.Errorf("profile %s: %w", b.Version, err)
	}

	return agent.Resolved{
		Model:         entry.Client,
		Dialect:       entry.Dialect,
		Registry:      registry,
		System:        b.System,
		Version:       b.Version,
		ModelName:     b.DefaultModel,
		MaxToolRounds: b.Doc.MaxToolRounds,
	}, nil
}

func (r *Runtime) validateCoreCapabilities(ctx context.Context, b *Built) error {
	for _, entry := range b.Enabled {
		if !strings.HasPrefix(entry.Capability, "agentk.") {
			continue
		}
		if r.catalog == nil {
			return fmt.Errorf(
				"profile %s: capability catalog is required for core tool capability %q",
				b.Version, entry.Capability)
		}
		resolved, err := r.catalog.ResolveTool(ctx, entry.Capability)
		if err != nil {
			return fmt.Errorf(
				"profile %s: resolve core tool capability %q: %w",
				b.Version, entry.Capability, err)
		}
		spec, err := specFromResolved(entry, resolved)
		if err == nil {
			if defaults, ok := b.Doc.ToolDefaults[entry.Name]; ok {
				_, err = tool.WithDefaults(spec, defaults)
			}
		}
		if err != nil {
			return fmt.Errorf(
				"profile %s: validate core tool capability %q: %w",
				b.Version, entry.Capability, err)
		}
	}
	return nil
}
