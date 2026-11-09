package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"agentk/internal/authz"
	corecapability "agentk/internal/capability"
	"agentk/internal/capcatalog"
	"agentk/internal/deltabuf"
	"agentk/internal/service"
	"agentk/internal/store/memory"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// testServerCapabilities 装配带能力目录的 API。目录用真实的 memory Catalog——
// 这条路径要保的正是"Worker 上报什么、接入方就看到什么"，桩掉目录会把它验空。
func testServerCapabilities(
	t *testing.T,
	directory service.CapabilityDirectory,
) *httptest.Server {
	t.Helper()
	st := memory.New()
	agentLoop := &fakeAgentLoop{store: st}
	appService := service.New(service.Config{
		Store: st, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
		Capabilities: directory,
	})
	srv := httptest.NewServer(authz.WithDevPrincipal(
		NewServer(ServerConfig{
			Service: appService,
			Deltas:  deltabuf.NewMemory(),
		}).Handler()))
	t.Cleanup(srv.Close)
	return srv
}

func announce(
	t *testing.T,
	catalog *capcatalog.Memory,
	taskQueue string,
	defs ...sdkcapability.CapabilityDef,
) {
	t.Helper()
	if _, err := catalog.Register(context.Background(), sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       taskQueue,
		Capabilities:    defs,
	}); err != nil {
		t.Fatal(err)
	}
}

func pipelineCapability(id, temporalType string) sdkcapability.CapabilityDef {
	return sdkcapability.CapabilityDef{
		ID: id, Kind: sdkcapability.CapabilityKindWorkflow,
		TemporalType: temporalType, Description: "描述 " + id,
		InputSchema:    json.RawMessage(`{"type":"object","required":["topic"]}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		TimeoutSeconds: 600,
	}
}

func getCapabilities(t *testing.T, url string) ([]capabilityDTO, string) {
	t.Helper()
	resp, err := http.Get(url + "/v1/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var out struct {
		Capabilities []capabilityDTO `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Capabilities, string(body)
}

// TestListCapabilitiesReturnsContractSortedByID 跨 task queue 汇总后按 id 升序，
// 且每条都带够写 Profile 所需的契约。
func TestListCapabilitiesReturnsContractSortedByID(t *testing.T) {
	catalog := capcatalog.NewMemory()
	announce(t, catalog, "queue-b", pipelineCapability("contrib.test.zeta", "Zeta"))
	announce(t, catalog, "queue-a", sdkcapability.CapabilityDef{
		ID: "contrib.test.alpha", Kind: sdkcapability.CapabilityKindActivity,
		TemporalType: "Alpha", Description: "阿尔法",
		InputSchema:    json.RawMessage(`{"type":"object"}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		TimeoutSeconds: 30,
		Tool: &sdkcapability.ToolDecl{
			Mode: sdkcapability.ToolModeBlocking, MaxAttempts: 3,
		},
	})

	got, _ := getCapabilities(t, testServerCapabilities(t, catalog).URL)
	if len(got) != 2 {
		t.Fatalf("capabilities = %+v", got)
	}
	if got[0].ID != "contrib.test.alpha" || got[1].ID != "contrib.test.zeta" {
		t.Fatalf("not sorted by id: %q, %q", got[0].ID, got[1].ID)
	}

	alpha := got[0]
	if alpha.Kind != "activity" || alpha.Description != "阿尔法" ||
		alpha.TimeoutSeconds != 30 || alpha.Source != "contrib" ||
		alpha.LastReportedAt == nil || alpha.LastReportedAt.IsZero() {
		t.Fatalf("alpha = %+v", alpha)
	}
	if alpha.Tool == nil || alpha.Tool.Mode != "blocking" ||
		alpha.Tool.Description != "阿尔法" || alpha.Tool.MaxAttempts != 3 {
		t.Fatalf("alpha tool = %+v", alpha.Tool)
	}
	zeta := got[1]
	if zeta.Tool != nil {
		t.Fatalf("zeta tool = %+v", zeta.Tool)
	}
	if !json.Valid(zeta.InputSchema) ||
		!strings.Contains(string(zeta.InputSchema), "topic") {
		t.Fatalf("zeta inputSchema = %s", zeta.InputSchema)
	}
	_, body := getCapabilities(t, testServerCapabilities(t, catalog).URL)
	for _, removed := range []string{"toolRef", "pipelineRef", `"pipeline"`, `"name"`} {
		if strings.Contains(body, removed) {
			t.Fatalf("response contains removed capability field %q: %s", removed, body)
		}
	}
}

// TestListCapabilitiesHidesTemporalRouting 响应只有契约：能力落在哪个 task queue、
// 对应哪个 workflow type 是内部执行细节，泄漏出去会让调用方产生错误依赖。
func TestListCapabilitiesHidesTemporalRouting(t *testing.T) {
	catalog := capcatalog.NewMemory()
	announce(t, catalog, "secret-queue",
		pipelineCapability("contrib.test.zeta", "SecretWorkflowType"))

	_, body := getCapabilities(t, testServerCapabilities(t, catalog).URL)
	for _, leaked := range []string{
		"secret-queue", "SecretWorkflowType", "taskQueue", "temporalType",
		"namespace", "digest",
	} {
		if strings.Contains(body, leaked) {
			t.Errorf("response leaks %q: %s", leaked, body)
		}
	}
}

// TestListCapabilitiesEmptyCatalog 没有 Worker 在报时是空列表而不是 null：
// 客户端可以无条件迭代。
func TestListCapabilitiesEmptyCatalog(t *testing.T) {
	got, body := getCapabilities(t, testServerCapabilities(t, capcatalog.NewMemory()).URL)
	if len(got) != 0 {
		t.Fatalf("capabilities = %+v", got)
	}
	if !strings.Contains(body, `"capabilities":[]`) {
		t.Fatalf("body = %s", body)
	}
}

func TestListCapabilitiesIncludesStaticCore(t *testing.T) {
	manifest, err := corecapability.CoreManifest()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := capcatalog.NewComposite(manifest, capcatalog.NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	got, body := getCapabilities(t, testServerCapabilities(t, catalog).URL)
	wantIDs := []string{
		"agentk.compose_video",
		"agentk.edit_image",
		"agentk.extract_audio",
		"agentk.extract_frame",
		"agentk.fetch_product_detail",
		"agentk.generate_brand_ad",
		"agentk.generate_image",
		"agentk.generate_speech_cosyvoice",
		"agentk.generate_speech_minimax",
		"agentk.generate_speech_qwen",
		"agentk.generate_text",
		"agentk.generate_video",
		"agentk.kling_image_to_video",
		"agentk.kling_omni_video",
		"agentk.kling_text_to_video",
		"agentk.probe_video",
		"agentk.process_video",
		"agentk.read_file",
		"agentk.seedance_generate_video",
		"agentk.transcribe_audio",
		"agentk.understand_media",
		"agentk.understand_video_segment",
		"agentk.web_search",
	}
	if len(got) != len(wantIDs) {
		t.Fatalf("capabilities = %+v", got)
	}
	for i, want := range wantIDs {
		if !strings.Contains(body, `"id":"`+want+`"`) {
			t.Errorf("response missing %q: %s", want, body)
		}
		if got[i].Source != "core" || got[i].LastReportedAt != nil {
			t.Errorf("core capability = %+v", got[i])
		}
		switch want {
		case "agentk.generate_image", "agentk.edit_image", "agentk.understand_media",
			"agentk.understand_video_segment", "agentk.web_search",
			"agentk.generate_speech_cosyvoice", "agentk.generate_speech_minimax",
			"agentk.generate_speech_qwen", "agentk.transcribe_audio",
			"agentk.fetch_product_detail":
			if got[i].Tool == nil || len(got[i].InputSchema) == 0 {
				t.Errorf("static tool capability = %+v", got[i])
			}
		case "agentk.kling_image_to_video", "agentk.kling_omni_video",
			"agentk.kling_text_to_video", "agentk.seedance_generate_video":
			if got[i].Tool == nil || got[i].Tool.Mode != "blocking" {
				t.Errorf("workflow video capability = %+v", got[i])
			}
		}
	}
	if image := got[slices.Index(wantIDs, "agentk.generate_image")]; !strings.Contains(string(image.InputSchema), `"z-image-turbo"`) {
		t.Fatalf("generate_image schema = %s", image.InputSchema)
	}
	if strings.Contains(body, "lastReportedAt") {
		t.Fatalf("response leaks catalog freshness: %s", body)
	}
}

func TestListCapabilitiesWithoutDirectoryIsNotImplemented(t *testing.T) {
	doJSON(t, "GET", testServerCapabilities(t, nil).URL+"/v1/capabilities",
		nil, http.StatusNotImplemented, nil)
}

// failingDirectory 模拟共享存储读失败。
type failingDirectory struct{}

func (failingDirectory) ListCapabilities(
	context.Context,
) ([]capcatalog.ResolvedCapability, error) {
	return nil, errors.New("kedis is down")
}

// TestListCapabilitiesCatalogFailure 目录读不出来是上游故障，不是空目录——
// 返回 502 而不是让调用方把"暂时读不到"误当成"能力被删了"。
func TestListCapabilitiesCatalogFailure(t *testing.T) {
	srv := testServerCapabilities(t, failingDirectory{})
	doJSON(t, "GET", srv.URL+"/v1/capabilities", nil, http.StatusBadGateway, nil)
}
