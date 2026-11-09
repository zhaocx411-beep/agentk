package capability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var (
	testInputSchema  = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
	testOutputSchema = json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`)
)

func validCapabilityDef(id string) CapabilityDef {
	return CapabilityDef{
		ID:             id,
		Kind:           CapabilityKindWorkflow,
		TemporalType:   id,
		Description:    "测试能力",
		InputSchema:    testInputSchema,
		OutputSchema:   testOutputSchema,
		TimeoutSeconds: 60,
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{
		"agentk.generate_brand_ad",
		"contrib.storyboard.film_go",
		"contrib.example.summarize",
		"agentk." + strings.Repeat("a", 121),
	} {
		if err := ValidateID(id); err != nil {
			t.Errorf("valid id %q rejected: %v", id, err)
		}
	}
	for _, id := range []string{
		"contrib.storyboard_film_go",
		"example.summarize",
		"agentk.brandad.generate",
		"agentk.generate-brand-ad",
		"AgentK.generate_brand_ad",
		"agentk",
		"agentk." + strings.Repeat("a", 122),
	} {
		if err := ValidateID(id); err == nil {
			t.Errorf("invalid id %q accepted", id)
		}
	}
}

func TestManifestNormalizesSortsAndInheritsDescriptions(t *testing.T) {
	zeta := validCapabilityDef("contrib.demo.zeta")
	alpha := validCapabilityDef("contrib.demo.alpha")
	alpha.Tool = &ToolDecl{Mode: ToolModeBlocking}

	manifest, err := (ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       "demo-queue",
		Capabilities:    []CapabilityDef{zeta, alpha},
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Capabilities) != 2 ||
		manifest.Capabilities[0].ID != "contrib.demo.alpha" ||
		manifest.Capabilities[1].ID != "contrib.demo.zeta" {
		t.Fatalf("capabilities = %+v", manifest.Capabilities)
	}
	if got := manifest.Capabilities[0].Tool.Description; got != "测试能力" {
		t.Fatalf("tool description = %q", got)
	}
}

func TestManifestNormalizeAllowsAgentKCapability(t *testing.T) {
	manifest, err := (ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       "agentk",
		Capabilities:    []CapabilityDef{validCapabilityDef("agentk.summarize")},
	}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Capabilities[0].ID != "agentk.summarize" {
		t.Fatalf("capability id = %q", manifest.Capabilities[0].ID)
	}
}

func TestManifestNormalizeReturnsDeepCopy(t *testing.T) {
	def := validCapabilityDef("contrib.demo.copy")
	source := ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       "demo-queue",
		Capabilities:    []CapabilityDef{def},
	}
	first, err := source.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	first.Capabilities[0].InputSchema[0] = 'x'

	second, err := source.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(second.Capabilities[0].InputSchema) {
		t.Fatalf("input schema mutated: %s", second.Capabilities[0].InputSchema)
	}
}

func TestManifestRejectsDuplicateCapabilityIDs(t *testing.T) {
	manifest := ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       "demo-queue",
		Capabilities: []CapabilityDef{
			validCapabilityDef("contrib.demo.one"),
			validCapabilityDef("contrib.demo.one"),
		},
	}
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate capability id") {
		t.Fatalf("error = %v", err)
	}
}

func TestManifestRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ManifestV1, *CapabilityDef)
		want string
	}{
		{name: "protocol", edit: func(m *ManifestV1, _ *CapabilityDef) { m.ProtocolVersion = 2 }, want: "protocolVersion"},
		{name: "task queue", edit: func(m *ManifestV1, _ *CapabilityDef) { m.TaskQueue = "" }, want: "taskQueue"},
		{name: "lane", edit: func(m *ManifestV1, _ *CapabilityDef) { m.LaneID = "dev lane" }, want: "laneId"},
		{name: "id", edit: func(_ *ManifestV1, d *CapabilityDef) { d.ID = "Bad ID" }, want: "must match"},
		{name: "kind", edit: func(_ *ManifestV1, d *CapabilityDef) { d.Kind = "service" }, want: "unknown kind"},
		{name: "type", edit: func(_ *ManifestV1, d *CapabilityDef) { d.TemporalType = "" }, want: "temporalType"},
		{name: "description", edit: func(_ *ManifestV1, d *CapabilityDef) { d.Description = "" }, want: "description"},
		{name: "timeout", edit: func(_ *ManifestV1, d *CapabilityDef) { d.TimeoutSeconds = 86401 }, want: "timeoutSeconds"},
		{name: "input", edit: func(_ *ManifestV1, d *CapabilityDef) { d.InputSchema = json.RawMessage(`{"type":"string"}`) }, want: "inputSchema"},
		{name: "output", edit: func(_ *ManifestV1, d *CapabilityDef) { d.OutputSchema = json.RawMessage(`{`) }, want: "outputSchema"},
		{
			name: "background tool mode",
			edit: func(_ *ManifestV1, d *CapabilityDef) {
				d.Tool = &ToolDecl{Mode: "background"}
			},
			want: "unknown mode",
		},
		{
			name: "tool mode",
			edit: func(_ *ManifestV1, d *CapabilityDef) {
				d.Tool = &ToolDecl{Mode: "detached"}
			},
			want: "unknown mode",
		},
		{
			name: "tool max attempts",
			edit: func(_ *ManifestV1, d *CapabilityDef) {
				d.Tool = &ToolDecl{
					Mode:        ToolModeBlocking,
					MaxAttempts: -1,
				}
			},
			want: "maxAttempts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := validCapabilityDef("contrib.demo.valid")
			manifest := ManifestV1{
				ProtocolVersion: ManifestProtocolVersion,
				TaskQueue:       "demo-queue",
				Capabilities:    []CapabilityDef{def},
			}
			tt.edit(&manifest, &manifest.Capabilities[0])
			if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestToolMaxAttemptsJSONOmission(t *testing.T) {
	withoutLimit, err := json.Marshal(ToolDecl{
		Mode: ToolModeBlocking,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(withoutLimit, []byte("maxAttempts")) {
		t.Fatalf("zero maxAttempts must be omitted: %s", withoutLimit)
	}

	withLimit, err := json.Marshal(ToolDecl{
		Mode: ToolModeBlocking, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(withLimit, []byte(`"maxAttempts":3`)) {
		t.Fatalf("positive maxAttempts missing: %s", withLimit)
	}
}

func TestManifestJSONIsStable(t *testing.T) {
	def := validCapabilityDef("contrib.demo.stable")
	def.Tool = &ToolDecl{Mode: ToolModeBlocking}
	source := ManifestV1{
		ProtocolVersion: ManifestProtocolVersion,
		TaskQueue:       "demo-queue",
		Capabilities:    []CapabilityDef{def},
	}
	first, err := source.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		next, err := source.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		nextJSON, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(firstJSON, nextJSON) {
			t.Fatalf("manifest changed:\n%s\n%s", firstJSON, nextJSON)
		}
	}
}
