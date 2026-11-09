package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/pipeline"
	"agentk/internal/service"
	"agentk/internal/store/memory"
	"agentk/internal/types"
)

/* ---------- 桩件：PipelineRunner（不触达 Temporal，直接写事件日志事件） ---------- */

// stubPipelineRunner 模拟 pipeline.Starter 的对外行为：List 返回固定声明，
// Start 校验名字后落 started + completed 事件（echo 回显 args）——与生产
// 一样，exec 的对外事实只有事件，查询端点 fold 事件回答。
type stubPipelineRunner struct {
	store    *memory.Store
	starts   int
	listErr  error
	startErr error
}

func (s *stubPipelineRunner) List(context.Context, types.Session) ([]pipeline.ListItem, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []pipeline.ListItem{{
		CapabilityID: "contrib.test.echo_topic",
		Name:         "echo_topic", DisplayName: "主题回显",
		Description:  "回显输入主题",
		InputSchema:  json.RawMessage(`{"type":"object","required":["topic"]}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["echoed"]}`),
	}}, nil
}

func (s *stubPipelineRunner) Start(ctx context.Context, sess types.Session, name string, args json.RawMessage) (types.Execution, error) {
	if s.startErr != nil {
		return types.Execution{}, s.startErr
	}
	if name != "echo_topic" {
		return types.Execution{}, fmt.Errorf("pipeline: %w: %q is not exposed", types.ErrNotFound, name)
	}
	s.starts++
	executionID := types.NewExecutionID()
	if _, err := s.store.AppendEvents(ctx, sess.ID, executionID, event.BatchID(executionID+"/started"),
		[]event.Draft{{Payload: event.ExecutionStarted{
			ExecutionKind:  types.ExecutionKindPipeline,
			ProfileVersion: "p-api-v1",
			Pipeline: &event.PipelineStarted{
				Name: name, CapabilityID: "contrib.test.echo_topic", Args: event.RawJSON(args),
			},
		}}}); err != nil {
		return types.Execution{}, err
	}
	// 模拟 workflow 侧完成：echo 原样回显 args.topic
	var in struct {
		Topic string `json:"topic"`
	}
	_ = json.Unmarshal(args, &in)
	output, _ := json.Marshal(map[string]string{"echoed": in.Topic})
	if _, err := s.store.AppendEvents(ctx, sess.ID, executionID, event.BatchID(executionID+"/finished"),
		[]event.Draft{{Payload: event.ExecutionFinished{
			Status:   types.ExecutionStatusCompleted,
			Pipeline: &event.PipelineFinished{Output: event.RawJSON(output)},
		}}}); err != nil {
		return types.Execution{}, err
	}
	return types.Execution{
		ID:              executionID,
		SessionID:       sess.ID,
		SessionPublicID: sess.PublicID,
		TenantID:        sess.TenantID,
		Kind:            types.ExecutionKindPipeline,
		Pipeline: &types.PipelineExecution{
			Name: name, CapabilityID: "contrib.test.echo_topic", Args: args,
		},
		Status:         types.ExecutionStatusRunning,
		ProfileVersion: "p-api-v1",
		CreatedAt:      time.Now(),
	}, nil
}

// testServerPipeline 装配带 pipeline 支路的 API（内存存储 + 桩 PipelineRunner）。
func testServerPipeline(t *testing.T) *httptest.Server {
	t.Helper()
	return testServerPipelineWithRunner(t, &stubPipelineRunner{})
}

func testServerPipelineWithRunner(
	t *testing.T,
	runner *stubPipelineRunner,
) *httptest.Server {
	t.Helper()
	st := memory.New()
	runner.store = st
	agentLoop := &fakeAgentLoop{store: st}
	appService := service.New(service.Config{
		Store: st, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
		Pipelines: runner,
	})
	srv := httptest.NewServer(authz.WithDevPrincipal(
		NewServer(ServerConfig{
			Service: appService,
			Deltas:  deltabuf.NewMemory(),
		}).Handler()))
	t.Cleanup(srv.Close)
	return srv
}

func TestPipelineUnavailableMapsToServiceUnavailable(t *testing.T) {
	runner := &stubPipelineRunner{}
	srv := testServerPipelineWithRunner(t, runner)
	var sess sessionDTO
	doJSON(t, "POST", srv.URL+"/v1/sessions",
		map[string]any{"title": "unavailable", "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sess)

	runner.listErr = fmt.Errorf("%w: catalog down", pipeline.ErrUnavailable)
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipelines",
		nil, http.StatusServiceUnavailable, nil)

	runner.listErr = nil
	runner.startErr = fmt.Errorf("%w: manifest expired", pipeline.ErrUnavailable)
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"pipeline": "echo_topic", "args": map[string]any{"topic": "x"}},
		http.StatusServiceUnavailable, nil)

	runner.startErr = types.ErrExecutionActive
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"pipeline": "echo_topic", "args": map[string]any{"topic": "x"}},
		http.StatusConflict, nil)
}

// TestPipelineEndpoints 走全链路：发现 → 触发 → 查询终态（exec 级读模型，
// 节点级进度已随 DSL 退役删除）。
func TestPipelineEndpoints(t *testing.T) {
	srv := testServerPipeline(t)

	var sess sessionDTO
	doJSON(t, "POST", srv.URL+"/v1/sessions",
		map[string]any{"title": "p api", "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sess)

	// 发现端点只返回公开契约，不泄漏 Temporal route。
	var listed struct {
		Pipelines []struct {
			CapabilityID string          `json:"capabilityId"`
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"inputSchema"`
			OutputSchema json.RawMessage `json:"outputSchema"`
			Kind         string          `json:"kind"`
			Type         string          `json:"type"`
		} `json:"pipelines"`
	}
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipelines", nil, http.StatusOK, &listed)
	if len(listed.Pipelines) != 1 || listed.Pipelines[0].Name != "echo_topic" ||
		listed.Pipelines[0].CapabilityID != "contrib.test.echo_topic" {
		t.Fatalf("pipelines = %+v", listed.Pipelines)
	}
	if listed.Pipelines[0].Description == "" || len(listed.Pipelines[0].InputSchema) == 0 ||
		len(listed.Pipelines[0].OutputSchema) == 0 {
		t.Fatalf("pipeline contract incomplete: %+v", listed.Pipelines[0])
	}
	if listed.Pipelines[0].Kind != "" || listed.Pipelines[0].Type != "" {
		t.Fatalf("pipeline route leaked: %+v", listed.Pipelines[0])
	}

	// 触发后 Temporal 接受即返回 202 和 Execution ID。
	var created struct {
		ID           string `json:"id"`
		CapabilityID string `json:"capabilityId"`
		Status       string `json:"status"`
	}
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"pipeline": "echo_topic", "args": map[string]any{"topic": "宇宙"}},
		http.StatusAccepted, &created)
	if created.ID == "" || created.CapabilityID != "contrib.test.echo_topic" ||
		created.Status != string(types.ExecutionStatusRunning) {
		t.Fatalf("created = %+v", created)
	}

	// 未暴露/未注册的 pipeline → 404
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"pipeline": "nope"}, http.StatusNotFound, nil)
	// pipeline 名缺失 → 400
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"args": map[string]any{}}, http.StatusBadRequest, nil)

	// 状态查询主通道（session-scoped，fold 会话事件）：exec 级状态 + 聚合输出
	var got struct {
		CapabilityID string          `json:"capabilityId"`
		Status       string          `json:"status"`
		Output       json.RawMessage `json:"output"`
		Nodes        json.RawMessage `json:"nodes"`
	}
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs/"+created.ID, nil, http.StatusOK, &got)
	if got.Status != string(types.ExecutionStatusCompleted) ||
		got.CapabilityID != "contrib.test.echo_topic" {
		t.Fatalf("exec status = %q", got.Status)
	}
	var out struct {
		Echoed string `json:"echoed"`
	}
	if err := json.Unmarshal(got.Output, &out); err != nil || out.Echoed != "宇宙" {
		t.Fatalf("output = %s (err %v)", got.Output, err)
	}
	if len(got.Nodes) != 0 {
		t.Fatalf("nodes must be gone from DTO, got %s", got.Nodes)
	}
	// 不存在的 run → 404
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs/exe_missing",
		nil, http.StatusNotFound, nil)

	// 会话维度列表：重开会话恢复卡片的数据源
	var runs struct {
		PipelineRuns []struct {
			ID           string          `json:"id"`
			Name         string          `json:"name"`
			CapabilityID string          `json:"capabilityId"`
			Status       string          `json:"status"`
			Args         json.RawMessage `json:"args"`
		} `json:"pipelineRuns"`
	}
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs", nil, http.StatusOK, &runs)
	if len(runs.PipelineRuns) != 1 {
		t.Fatalf("pipelineRuns = %+v", runs.PipelineRuns)
	}
	if r := runs.PipelineRuns[0]; r.ID != created.ID || r.Name != "echo_topic" ||
		r.CapabilityID != "contrib.test.echo_topic" ||
		r.Status != string(types.ExecutionStatusCompleted) || len(r.Args) == 0 {
		t.Fatalf("pipelineRuns[0] = %+v", runs.PipelineRuns[0])
	}
}

// TestPipelineEndpointsDisabled 未装配 Pipelines 时 pipeline 端点返回 501。
func TestPipelineEndpointsDisabled(t *testing.T) {
	st := memory.New()
	agentLoop := &fakeAgentLoop{store: st}
	appService := service.New(service.Config{
		Store: st, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
	})
	srv := httptest.NewServer(authz.WithDevPrincipal(
		NewServer(ServerConfig{
			Service: appService,
			Deltas:  deltabuf.NewMemory(),
		}).Handler()))
	t.Cleanup(srv.Close)

	var sess sessionDTO
	doJSON(t, "POST", srv.URL+"/v1/sessions",
		map[string]any{"title": "no pipelines", "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sess)
	doJSON(t, "GET", srv.URL+"/v1/sessions/"+sess.ID+"/pipelines", nil, http.StatusNotImplemented, nil)
	doJSON(t, "POST", srv.URL+"/v1/sessions/"+sess.ID+"/pipeline-runs",
		map[string]any{"pipeline": "echo_topic"}, http.StatusNotImplemented, nil)
}
