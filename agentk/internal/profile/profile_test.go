package profile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentk/internal/capability"
	"agentk/internal/capcatalog"
	"agentk/internal/modelcatalog"
	"agentk/internal/tool"
)

func localEcho() tool.Spec {
	return tool.Spec{
		Name: "echo", Description: "回显",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.InlineBinding{Execute: func(
			context.Context, json.RawMessage, tool.Context,
		) (any, error) {
			return map[string]bool{"ok": true}, nil
		}},
	}
}

func testModels(t *testing.T, model string) *modelcatalog.Catalog {
	t.Helper()
	cat, err := modelcatalog.Parse([]byte(
		"models:\n  - {name: "+model+", baseUrl: https://chat.test/v1}\n",
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func testCapabilityCatalog(t *testing.T) capcatalog.Catalog {
	t.Helper()
	dynamic, err := capcatalog.New("mem")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := capability.CoreManifest()
	if err != nil {
		t.Fatal(err)
	}
	composite, err := capcatalog.NewComposite(manifest, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	return composite
}

func testRuntime(t *testing.T, models *modelcatalog.Catalog, local ...tool.Spec) *Runtime {
	t.Helper()
	return NewRuntime(modelcatalog.NewActive(models), testCapabilityCatalog(t), local)
}

func TestParseRequiresExplicitDefaultModel(t *testing.T) {
	if _, err := Parse([]byte(`{"system":"s","tools":[]}`), []tool.Spec{localEcho()}); err == nil ||
		!strings.Contains(err.Error(), "defaultModel is required") {
		t.Fatalf("error = %v", err)
	}
	built, err := Parse([]byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[{"name":"echo"}]}`,
	), []tool.Spec{localEcho()})
	if err != nil {
		t.Fatal(err)
	}
	if built.DefaultModel != "deepseek-v4-pro" || len(built.Enabled) != 1 {
		t.Fatalf("built = %+v", built)
	}
	if _, err := Parse([]byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[`+
			`{"capability":"","name":"echo"}]}`,
	), []tool.Spec{localEcho()}); err != nil {
		t.Fatalf("explicit empty capability must select local tool: %v", err)
	}
	if _, err := Parse([]byte(
		`{"defaultModel":"invented","system":"s","tools":[]}`,
	), nil); err == nil {
		t.Fatal("unknown logical model must be rejected")
	}
}

func TestParseMaxToolRounds(t *testing.T) {
	built, err := Parse([]byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[],"maxToolRounds":50}`,
	), nil)
	if err != nil {
		t.Fatal(err)
	}
	if built.Doc.MaxToolRounds != 50 {
		t.Fatalf("MaxToolRounds = %d, want 50", built.Doc.MaxToolRounds)
	}
	if _, err := Parse([]byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[],"maxToolRounds":-1}`,
	), nil); err == nil || !strings.Contains(err.Error(), "maxToolRounds") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolvePropagatesMaxToolRounds(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"))
	resolved, err := rt.Resolve(context.Background(), []byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[],"maxToolRounds":7}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MaxToolRounds != 7 {
		t.Fatalf("MaxToolRounds = %d, want 7", resolved.MaxToolRounds)
	}
}

func TestParseRejectsRemovedHTTPToolsContract(t *testing.T) {
	_, err := Parse([]byte(`{
		"defaultModel":"deepseek-v4-pro",
		"system":"",
		"tools":[],
		"httpTools":[{"name":"legacy","http":{"url":"https://example.com"}}]
	}`), []tool.Spec{localEcho()})
	if err == nil || !strings.Contains(err.Error(), `unknown field "httpTools"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestSynchronousCapabilityRequiresCapabilityID(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"), localEcho())
	oldName := json.RawMessage(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[{"name":"generate_image"}]}`,
	)
	if _, err := rt.MaterializeSessionProfile(oldName); err == nil ||
		!strings.Contains(err.Error(), `unknown tool "generate_image"`) {
		t.Fatalf("error = %v", err)
	}

	withCapability := json.RawMessage(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
			`{"capability":"agentk.generate_image","name":"generate_image"}]}`,
	)
	if _, err := rt.MaterializeSessionProfile(withCapability); err != nil {
		t.Fatal(err)
	}
}

func TestFixedMediaCapabilityRequiresCapabilityID(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"), localEcho())
	oldName := json.RawMessage(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[{"name":"probe_video"}]}`,
	)
	if _, err := rt.MaterializeSessionProfile(oldName); err == nil {
		t.Fatal("fixed media name-only path must be rejected")
	}
	withCapability := json.RawMessage(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
			`{"capability":"agentk.probe_video","name":"probe_video"}]}`,
	)
	if _, err := rt.MaterializeSessionProfile(withCapability); err != nil {
		t.Fatal(err)
	}
}

func TestToolCapabilityRejectsPrefixedID(t *testing.T) {
	_, err := Parse([]byte(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[`+
			`{"capability":"tool:agentk.generate_image","name":"generate_image"}]}`,
	), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid capability") {
		t.Fatalf("error = %v", err)
	}
}

func TestLegacyCapabilityRefsNormalize(t *testing.T) {
	built, err := Parse([]byte(`{
		"defaultModel":"deepseek-v4-pro",
		"system":"s",
		"tools":[{
			"ref":"tool:agentk.generate_image",
			"name":"generate_image"
		}],
		"pipelines":[{
			"ref":"pipeline:contrib.example.render",
			"name":"render"
		}]
	}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := built.Doc.Tools[0]; got.Capability != "agentk.generate_image" || got.Ref != "" {
		t.Fatalf("tool = %+v", got)
	}
	if got := built.Doc.Pipelines[0]; got.Capability != "contrib.example.render" || got.Ref != "" {
		t.Fatalf("pipeline = %+v", got)
	}
	if strings.Contains(string(built.Raw), `"ref"`) {
		t.Fatalf("canonical profile retains legacy ref: %s", built.Raw)
	}
}

func TestCapabilityAndLegacyRefConflict(t *testing.T) {
	for _, raw := range []string{
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[{
			"capability":"agentk.generate_image",
			"ref":"tool:agentk.generate_image",
			"name":"generate_image"
		}]}`,
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[],"pipelines":[{
			"capability":"contrib.example.render",
			"ref":"pipeline:contrib.example.render",
			"name":"render"
		}]}`,
	} {
		if _, err := Parse([]byte(raw), nil); err == nil ||
			!strings.Contains(err.Error(), "capability and ref cannot both be set") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestResolveCoreToolUsesStaticManifestAndCoreQueue(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"), localEcho())
	raw := []byte(`{
		"defaultModel":"deepseek-v4-pro",
		"system":"s",
		"tools":[{
			"capability":"agentk.generate_image",
			"name":"image",
			"instructions":"Prefer concise prompts."
		}],
		"toolDefaults":{"image":{"aspectRatio":"1:1"}}
	}`)
	resolved, err := rt.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MaxToolRounds != 0 {
		t.Fatalf("MaxToolRounds = %d, want 0 (default)", resolved.MaxToolRounds)
	}
	spec, ok := resolved.Registry.Get("image")
	if !ok {
		t.Fatal("resolved tool missing")
	}
	binding, ok := spec.Binding.(tool.ActivityBinding)
	if !ok || binding.TaskQueue != "agentk" {
		t.Fatalf("binding = %+v", spec.Binding)
	}
	schema := string(spec.InputSchema)
	if !strings.Contains(schema, `"z-image-turbo"`) ||
		!strings.Contains(schema, `"gpt-image-2"`) {
		t.Fatalf("static schema = %s", schema)
	}
	if !strings.Contains(spec.Description, "Prefer concise prompts.") {
		t.Fatalf("description = %q", spec.Description)
	}
}

func TestCoreCapabilityFailsClosed(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"), localEcho())
	raw := json.RawMessage(
		`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
			`{"capability":"agentk.not_real","name":"missing"}]}`,
	)
	if _, err := rt.MaterializeSessionProfile(raw); err == nil {
		t.Fatal("missing core capability must fail")
	}
}

func TestKnownButUnconfiguredChatFailsOnlyAtResolve(t *testing.T) {
	rt := testRuntime(t, testModels(t, "deepseek-v4-pro"), localEcho())
	raw := json.RawMessage(
		`{"defaultModel":"qwen3.5-plus","system":"s","tools":[]}`,
	)
	if _, err := rt.MaterializeSessionProfile(raw); err != nil {
		t.Fatalf("materialize must not depend on current model config: %v", err)
	}
	if _, err := rt.Resolve(context.Background(), raw); err == nil ||
		!strings.Contains(err.Error(), `model "qwen3.5-plus" not available`) {
		t.Fatalf("resolve error = %v", err)
	}
}

func TestModelHotSwapDoesNotChangeToolSchema(t *testing.T) {
	first := testModels(t, "deepseek-v4-pro")
	active := modelcatalog.NewActive(first)
	rt := NewRuntime(active, testCapabilityCatalog(t), []tool.Spec{localEcho()})
	raw := []byte(`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
		`{"capability":"agentk.generate_image","name":"image"}]}`)

	before, err := rt.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	beforeSpec, _ := before.Registry.Get("image")

	second := testModels(t, "qwen3.5-plus")
	active.Swap(second)
	afterRaw := []byte(`{"defaultModel":"qwen3.5-plus","system":"s","tools":[` +
		`{"capability":"agentk.generate_image","name":"image"}]}`)
	after, err := rt.Resolve(context.Background(), afterRaw)
	if err != nil {
		t.Fatal(err)
	}
	afterSpec, _ := after.Registry.Get("image")
	if string(beforeSpec.InputSchema) != string(afterSpec.InputSchema) {
		t.Fatal("model hot reload changed static tool schema")
	}
}

func TestVideoToolUsesCapability(t *testing.T) {
	models := testModels(t, "deepseek-v4-pro")
	rt := testRuntime(t, models, localEcho())
	raw := []byte(`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
		`{"capability":"agentk.kling_image_to_video","name":"video"}],` +
		`"toolDefaults":{"video":{"model":"kling-3.0"}}}`)
	resolved, err := rt.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := resolved.Registry.Get("video")
	if !ok {
		t.Fatal("video tool missing from registry")
	}
	binding, ok := spec.Binding.(tool.WorkflowBinding)
	if !ok || binding.WorkflowType != "agentk.capability.kling_image_to_video" ||
		binding.TaskQueue != "agentk" {
		t.Fatalf("binding = %+v", spec.Binding)
	}
	if string(spec.DefaultArgs) != `{"model":"kling-3.0"}` {
		t.Fatalf("defaults = %s", spec.DefaultArgs)
	}

	nameOnly := []byte(`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
		`{"name":"kling_image_to_video"}]}`)
	if _, err := rt.Resolve(context.Background(), nameOnly); err == nil {
		t.Fatal("name-only video tool must not resolve")
	}
}

func TestProductDetailToolUsesStaticCapability(t *testing.T) {
	models := testModels(t, "deepseek-v4-pro")
	rt := testRuntime(t, models, localEcho())
	raw := []byte(`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
		`{"capability":"agentk.fetch_product_detail","name":"product"}]}`)
	resolved, err := rt.Resolve(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := resolved.Registry.Get("product")
	if !ok {
		t.Fatal("product detail tool missing from registry")
	}
	binding, ok := spec.Binding.(tool.ActivityBinding)
	if !ok || binding.ActivityName != "agentk.capability.fetch_product_detail" ||
		binding.TaskQueue != "agentk" || binding.MaxAttempts != 1 {
		t.Fatalf("binding = %+v", spec.Binding)
	}

	nameOnly := []byte(`{"defaultModel":"deepseek-v4-pro","system":"s","tools":[` +
		`{"name":"fetch_product_detail"}]}`)
	if _, err := rt.Resolve(context.Background(), nameOnly); err == nil {
		t.Fatal("name-only product detail tool must not resolve")
	}
}
