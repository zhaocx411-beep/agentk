package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	operatorservice "go.temporal.io/api/operatorservice/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/durationpb"

	agentkworker "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	sdkinvocation "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"

	"agentk/internal/agent"
	"agentk/internal/agentloop"
	"agentk/internal/api"
	"agentk/internal/authz"
	"agentk/internal/capability"
	"agentk/internal/capability/brandad"
	"agentk/internal/capcatalog"
	"agentk/internal/chat"
	"agentk/internal/deltabuf"
	"agentk/internal/discovery"
	"agentk/internal/event"
	"agentk/internal/execution"
	invocationcallback "agentk/internal/invocation"
	"agentk/internal/modelcatalog"
	"agentk/internal/pipeline"
	"agentk/internal/profile"
	"agentk/internal/service"
	"agentk/internal/store/memory"
	"agentk/internal/tool"
	"agentk/internal/types"
)

func TestDialTemporalKESSIntegration(t *testing.T) {
	if os.Getenv("AGENTK_TEST_KESS") != "1" {
		t.Skip("AGENTK_TEST_KESS is not enabled")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	temporalClient, err := dialTemporal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer temporalClient.Close()
	if _, err := temporalClient.CheckHealth(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

type temporalRegistrationRecorder struct {
	workflows  []string
	activities []string
}

func (r *temporalRegistrationRecorder) RegisterWorkflowWithOptions(
	_ interface{},
	options workflow.RegisterOptions,
) {
	r.workflows = append(r.workflows, options.Name)
}

func (r *temporalRegistrationRecorder) RegisterActivityWithOptions(
	_ interface{},
	options activity.RegisterOptions,
) {
	r.activities = append(r.activities, options.Name)
}

// TestRegisterBusinessTemporal 注册面是发布契约：改名或漏注册都会让在飞的
// workflow 找不到实现，这里把名字与数量钉死。
func TestRegisterBusinessTemporal(t *testing.T) {
	recorder := &temporalRegistrationRecorder{}
	agentloop.Register(recorder,
		agentloop.Config{Store: memory.New(), WorkerID: "embedded-test"})
	if len(recorder.workflows) != 1 ||
		recorder.workflows[0] != agentloop.AgentWorkflowType {
		t.Fatalf("workflows = %v", recorder.workflows)
	}
	wantActivities := []string{
		agentloop.BeginExecutionActivityName,
		agentloop.FinishExecutionActivityName,
		agentloop.RoundActivityName,
		agentloop.ExecuteInlineToolActivityName,
		agentloop.BeginToolActivityName,
		agentloop.FinishToolActivityName,
		agentloop.AskUserActivityName,
	}
	if len(recorder.activities) != len(wantActivities) {
		t.Fatalf("activities = %v", recorder.activities)
	}
	for i, want := range wantActivities {
		if recorder.activities[i] != want {
			t.Fatalf("activity[%d] = %q, want %q", i, recorder.activities[i], want)
		}
	}
}

/* ---------- 脚本化模型 ---------- */

// scriptedTurn 是脚本模型的一次产出。
type scriptedTurn struct {
	text      string
	reasoning string
	toolCalls []chat.ToolCall
	// err 非 nil 时本次调用直接失败（验证重试与失败收场）
	err error
}

// scriptedModel 按调用序号照本宣科。它替代真实 provider：E2E 要验的是编排、
// 事件日志与推送，模型只需产出确定的东西。
type scriptedModel struct {
	mu    sync.Mutex
	turns []scriptedTurn
	calls atomic.Int32
	// keys 记录每次调用的幂等键（验证 Activity 重试时它不变）
	keys []string
	// seen 记录每次调用看到的历史长度
	seen []int
}

func (m *scriptedModel) Stream(_ context.Context, req chat.Request) (chat.Response, error) {
	n := int(m.calls.Add(1)) - 1
	m.mu.Lock()
	m.keys = append(m.keys, req.IdempotencyKey)
	m.seen = append(m.seen, len(req.Messages))
	turn := scriptedTurn{text: "（脚本已用尽）"}
	if n < len(m.turns) {
		turn = m.turns[n]
	}
	m.mu.Unlock()
	if turn.err != nil {
		return chat.Response{}, turn.err
	}

	var blocks []chat.Block
	var deltas []chat.Delta
	if turn.reasoning != "" {
		deltas = append(deltas, chat.Delta{Block: len(blocks), Kind: chat.BlockReasoning, Text: turn.reasoning})
		blocks = append(blocks, chat.Block{Kind: chat.BlockReasoning, Text: turn.reasoning})
	}
	if turn.text != "" {
		deltas = append(deltas, chat.Delta{Block: len(blocks), Kind: chat.BlockText, Text: turn.text})
		blocks = append(blocks, chat.Block{Kind: chat.BlockText, Text: turn.text})
	}
	finish := "stop"
	if len(turn.toolCalls) > 0 {
		finish = "tool_calls"
	}
	return chat.Response{
		Stream: func(yield func(chat.Delta, error) bool) {
			for _, d := range deltas {
				if !yield(d, nil) {
					return
				}
			}
		},
		Result: func() (chat.Result, error) {
			return chat.Result{
				Blocks:       blocks,
				ToolCalls:    turn.toolCalls,
				Usage:        chat.Usage{InputTokens: 2, OutputTokens: 3},
				FinishReason: finish,
			}, nil
		},
	}, nil
}

func (m *scriptedModel) snapshot() (keys []string, seen []int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.keys...), append([]int(nil), m.seen...)
}

type afterResultModel struct {
	delegate chat.Client
	after    func()
	once     sync.Once
}

func (m *afterResultModel) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	response, err := m.delegate.Stream(ctx, req)
	if err != nil || m.after == nil {
		return response, err
	}
	result := response.Result
	response.Result = func() (chat.Result, error) {
		out, resultErr := result()
		if resultErr == nil {
			m.once.Do(m.after)
		}
		return out, resultErr
	}
	return response, nil
}

type switchableCatalog struct {
	capcatalog.Catalog
	unavailable atomic.Bool
}

func (c *switchableCatalog) ResolveTool(
	ctx context.Context,
	capabilityID string,
) (capcatalog.ResolvedTool, error) {
	if c.unavailable.Load() {
		return capcatalog.ResolvedTool{}, fmt.Errorf("%w: disabled after tool call", capcatalog.ErrNotFound)
	}
	return c.Catalog.ResolveTool(ctx, capabilityID)
}

// blockingModel 首次调用即通知 started，然后阻塞到 context 取消——让取消信号在
// RoundActivity 在飞时到达。
type blockingModel struct {
	started chan struct{}
	once    sync.Once
}

func (m *blockingModel) Stream(ctx context.Context, _ chat.Request) (chat.Response, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return chat.Response{}, ctx.Err()
}

var _ iter.Seq2[chat.Delta, error] = chat.Response{}.Stream

/* ---------- E2E 装配 ---------- */

// e2eDefaultTemporalAddress 是 `temporal server start-dev` 的缺省监听。
// 不设 AGENTK_TEST_TEMPORAL_ADDRESS 时试连这里：dev server 开着就自动跑，
// 免得整组 E2E 因为没人记得设变量而长期静默 skip。
const (
	e2eDefaultTemporalAddress   = "127.0.0.1:7233"
	e2eDefaultTemporalNamespace = "default"
)

// e2eEnv 是 dev-server E2E 的公共装配：内存存储 + delta 缓冲 + 内嵌业务 worker +
// httptest 服务（dev principal）。缺省为每个用例创建独立 Temporal namespace，
// 使固定的 agentk queue 不会与本机其他 Worker 或相邻用例互相消费任务。
type e2eEnv struct {
	client            client.Client
	store             *memory.Store
	deltas            *deltabuf.Memory
	capabilityCatalog capcatalog.Catalog
	// taskQueue 业务 worker 的队列名（外部 pipeline 用例里 child 跨队列回调用）
	taskQueue  string
	httpServer *httptest.Server
	httpClient *http.Client
}

type e2eOptions struct {
	model    chat.Client
	registry *tool.Registry
	// dynamicProfile 使用生产 profile.Runtime，让 Profile capability 通过内存 Catalog
	// 逐轮解析；其余用例继续使用固定 Runtime 隔离各自关注点。
	dynamicProfile bool
	catalog        capcatalog.Catalog
	// pipelines 为真时挂 pipeline 支路
	pipelines bool
	// workflows 额外注册的 workflow（WorkflowBinding 工具的执行体），按名注册
	// 进业务 worker。
	workflows map[string]func(workflow.Context, map[string]any) (json.RawMessage, error)
}

func newE2EEnv(t *testing.T, opts e2eOptions) *e2eEnv {
	t.Helper()
	address := strings.TrimSpace(os.Getenv("AGENTK_TEST_TEMPORAL_ADDRESS"))
	explicitAddress := address != ""
	if !explicitAddress {
		address = e2eDefaultTemporalAddress
	}
	namespace := strings.TrimSpace(os.Getenv("AGENTK_TEST_TEMPORAL_NAMESPACE"))
	if namespace == "" {
		var err error
		namespace, err = createE2ETemporalNamespace(t, address)
		if err != nil {
			if explicitAddress {
				t.Fatal(err)
			}
			t.Skipf("no Temporal at %s: run `temporal server start-dev` or point "+
				"AGENTK_TEST_TEMPORAL_ADDRESS elsewhere (%v)", address, err)
		}
	}
	temporalCfg := temporalConfig{
		TaskQueue: temporalTaskQueue,
	}
	temporalClient, err := client.Dial(client.Options{
		HostPort:  address,
		Namespace: namespace,
		ContextPropagators: []workflow.ContextPropagator{
			sdkinvocation.MetaPropagator{},
		},
	})
	if err != nil {
		if explicitAddress {
			// 点名了地址却连不上是故障，不是"本机没这套环境"
			t.Fatal(err)
		}
		t.Skipf("no Temporal at %s: run `temporal server start-dev` or point "+
			"AGENTK_TEST_TEMPORAL_ADDRESS elsewhere (%v)", address, err)
	}
	t.Cleanup(temporalClient.Close)

	if opts.registry == nil {
		opts.registry, err = tool.NewRegistry()
		if err != nil {
			t.Fatal(err)
		}
	}
	dynamicCapabilityDirectory := opts.catalog
	if dynamicCapabilityDirectory == nil {
		dynamicCapabilityDirectory = capcatalog.NewMemory()
	}
	coreManifest, err := capability.CoreManifest()
	if err != nil {
		t.Fatal(err)
	}
	capabilityDirectory, err := capcatalog.NewComposite(
		coreManifest, dynamicCapabilityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var runtime agent.Runtime = agent.StaticRuntime{
		Model: opts.model, Registry: opts.registry,
		Version: "e2e-v1", ModelName: "deepseek-v4-pro", Dialect: event.DialectOpenAI,
	}
	var sessionRuntime service.SessionProfileRuntime = e2eProfileRuntime{}
	activeCatalog := modelcatalog.NewActive(nil)
	if opts.dynamicProfile {
		cat := &modelcatalog.Catalog{
			Version: "e2e-v1",
			Chat: map[string]modelcatalog.ChatModel{
				"deepseek-v4-pro": {Client: opts.model, Dialect: event.DialectOpenAI},
			},
		}
		activeCatalog = modelcatalog.NewActive(cat)
		profileRuntime := profile.NewRuntime(
			activeCatalog, capabilityDirectory, opts.registry.Specs())
		runtime = profileRuntime
		sessionRuntime = profileRuntime
	}
	store := memory.New()
	deltas := deltabuf.NewMemory()
	loopCfg := agentloop.Config{
		Store: store, Runtime: runtime, WorkerID: "e2e-worker",
		Deltas: deltas,
	}
	starter := agentloop.NewStarter(temporalClient, temporalCfg.TaskQueue)

	pipelineActivities := pipeline.NewActivities(store, deltas)
	invocationActivities := invocationcallback.NewActivities(store)
	discoveryActivities := discovery.NewActivities(capabilityDirectory)
	businessWorker := newTemporalWorker(
		temporalClient, temporalCfg, loopCfg,
		activeCatalog, nil, pipelineActivities, invocationActivities,
		discoveryActivities)
	for name, fn := range opts.workflows {
		businessWorker.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: name})
	}
	if err := businessWorker.Start(); err != nil {
		t.Fatalf("start business Temporal worker: %v", err)
	}
	// t.Cleanup 后进先出：测试体内先等 workflow 全部终结，这里再停 worker，
	// 避免遗留 pending task 打到共享 dev server 的下一个测试。
	t.Cleanup(businessWorker.Stop)

	var pipelines service.PipelineRunner
	if opts.pipelines {
		pipelines = pipeline.NewStarter(pipeline.StarterConfig{
			Temporal: temporalClient, TaskQueue: temporalCfg.TaskQueue,
			Catalog: capabilityDirectory,
		})
	}
	agentkService := service.New(service.Config{
		Store:          store,
		AgentLoop:      starter,
		Executions:     execution.NewCanceller(temporalClient),
		ProfileRuntime: sessionRuntime,
		Pipelines:      pipelines,
		Capabilities:   capabilityDirectory,
	})
	// 与生产共用 handler 装配；E2E 无产物存储，/files 因而不挂。
	httpServer := httptest.NewServer(newRootHandler(api.ServerConfig{
		Service: agentkService,
		Deltas:  deltas,
	}, nil, authz.WithDevPrincipal))
	t.Cleanup(httpServer.Close)

	return &e2eEnv{
		client: temporalClient, store: store, deltas: deltas,
		capabilityCatalog: capabilityDirectory,
		taskQueue:         temporalCfg.TaskQueue,
		httpServer:        httpServer, httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func createE2ETemporalNamespace(t *testing.T, address string) (string, error) {
	t.Helper()
	admin, err := client.Dial(client.Options{
		HostPort:  address,
		Namespace: e2eDefaultTemporalNamespace,
	})
	if err != nil {
		return "", fmt.Errorf("dial Temporal namespace admin at %s: %w", address, err)
	}
	namespace := fmt.Sprintf("agentk-test-%d", time.Now().UnixNano())
	_, err = admin.WorkflowService().RegisterNamespace(
		t.Context(),
		&workflowservice.RegisterNamespaceRequest{
			Namespace: namespace,
			WorkflowExecutionRetentionPeriod: durationpb.New(
				24 * time.Hour,
			),
		},
	)
	if err != nil {
		admin.Close()
		return "", fmt.Errorf("register Temporal namespace %q: %w", namespace, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.OperatorService().DeleteNamespace(
			ctx,
			&operatorservice.DeleteNamespaceRequest{
				Namespace:            namespace,
				NamespaceDeleteDelay: durationpb.New(0),
			},
		); err != nil {
			t.Logf("delete Temporal namespace %q: %v", namespace, err)
		}
		admin.Close()
	})
	return namespace, nil
}

// e2eProfileRuntime 是接入层 profile 固化/复核的桩：只做形状校验，
// 能力校验由 StaticRuntime 的固定配置代表。
type e2eProfileRuntime struct{}

func (e2eProfileRuntime) MaterializeSessionProfile(requestJSON json.RawMessage) (json.RawMessage, error) {
	raw := bytes.TrimSpace(requestJSON)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("session profile is required")
	}
	if raw[0] != '{' || !json.Valid(raw) {
		return nil, fmt.Errorf("profile must be a JSON object")
	}
	return append(json.RawMessage(nil), raw...), nil
}

func (e2eProfileRuntime) ValidateSessionProfile([]byte) error { return nil }

func (e *e2eEnv) doJSON(t *testing.T, method, path string, body any, wantStatus int, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.httpServer.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status = %d, want %d; body: %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
	}
}

// newSession 建会话并返回会话 public ID。
func (e *e2eEnv) newSession(t *testing.T, title string) string {
	t.Helper()
	var session struct {
		ID string `json:"id"`
	}
	e.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title":   title,
		"profile": map[string]any{"system": "", "tools": []string{}},
	}, http.StatusCreated, &session)
	return session.ID
}

func (e *e2eEnv) send(t *testing.T, sessionID, text string) {
	t.Helper()
	e.doJSON(t, http.MethodPost, "/v1/sessions/"+sessionID+"/messages",
		map[string]string{"text": text}, http.StatusAccepted, nil)
}

// waitWorkflow 等 Session 当前 Execution 对应的 workflow 结束。
func (e *e2eEnv) waitWorkflow(t *testing.T, sessionID string, timeout time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return e.client.GetWorkflow(ctx, execution.WorkflowID(sessionID), "").Get(ctx, nil)
}

// waitPipelineCapability 等 Catalog 出现该 pipeline capability（Worker announce 是异步的）。
func (e *e2eEnv) waitPipelineCapability(
	t *testing.T, capabilityID string, timeout time.Duration,
) capcatalog.ResolvedCapability {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		resolved, err := e.capabilityCatalog.ResolveCapability(t.Context(), capabilityID)
		if err == nil {
			return resolved
		}
		if !errors.Is(err, capcatalog.ErrNotFound) || time.Now().After(deadline) {
			t.Fatalf("resolve pipeline capability %s: %v", capabilityID, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (e *e2eEnv) waitToolCapability(
	t *testing.T, capabilityID string, timeout time.Duration,
) capcatalog.ResolvedTool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		resolved, err := e.capabilityCatalog.ResolveTool(t.Context(), capabilityID)
		if err == nil {
			return resolved
		}
		if !errors.Is(err, capcatalog.ErrNotFound) || time.Now().After(deadline) {
			t.Fatalf("resolve tool capability %s: %v", capabilityID, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// startPipelineRun 触发一次 Pipeline 并返回 Execution ID。
func (e *e2eEnv) startPipelineRun(
	t *testing.T, sessionID, name string, args any,
) string {
	t.Helper()
	var started struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	e.doJSON(t, http.MethodPost, "/v1/sessions/"+sessionID+"/pipeline-runs",
		map[string]any{"pipeline": name, "args": args}, http.StatusAccepted, &started)
	if started.ID == "" || started.Status != string(types.ExecutionStatusRunning) {
		t.Fatalf("started exec = %+v", started)
	}
	return started.ID
}

// pipelineRunOutput 读一次执行的对外快照。
func (e *e2eEnv) pipelineRunOutput(t *testing.T, sessionID, execID string) (string, string) {
	t.Helper()
	var got struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
	}
	e.doJSON(t, http.MethodGet,
		"/v1/sessions/"+sessionID+"/pipeline-runs/"+execID, nil, http.StatusOK, &got)
	return got.Status, string(got.Output)
}

// waitEvents 轮询事件日志直到 cond 满足，返回当时的全部事件。
func (e *e2eEnv) waitEvents(
	t *testing.T, sessionID string, timeout time.Duration, cond func([]event.Event) bool,
) []event.Event {
	t.Helper()
	ctx := context.Background()
	stored, err := e.store.GetSessionByPublicID(ctx, "org_dev", sessionID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(timeout)
	var events []event.Event
	for time.Now().Before(deadline) {
		events, err = e.store.LoadSessionEvents(ctx, stored.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cond(events) {
			return events
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition not met in %s; events = %v", timeout, eventKinds(events))
	return nil
}

func eventKinds(events []event.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, string(ev.Payload.Kind()))
	}
	return out
}

func countKind(events []event.Event, want event.Kind) int {
	n := 0
	for _, ev := range events {
		if ev.Payload.Kind() == want {
			n++
		}
	}
	return n
}

func hasKind(events []event.Event, want event.Kind) bool {
	return countKind(events, want) > 0
}

// findPayload 返回第一条该类型的载荷。
func findPayload[T event.Payload](events []event.Event) (T, bool) {
	var zero T
	for _, ev := range events {
		if p, ok := ev.Payload.(T); ok {
			return p, true
		}
	}
	return zero, false
}

/* ---------- 用例：一问一答 ---------- */

// TestE2ESingleTurn 覆盖最小闭环：HTTP 发消息 → 启动 Execution →
// RoundActivity 调模型 → 事件批落库 → Execution 收尾。
func TestE2ESingleTurn(t *testing.T) {
	model := &scriptedModel{turns: []scriptedTurn{{text: "你好，我在"}}}
	env := newE2EEnv(t, e2eOptions{model: model})

	sessionID := env.newSession(t, "单轮")
	env.send(t, sessionID, "在吗")
	if err := env.waitWorkflow(t, sessionID, 60*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}

	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return hasKind(evs, event.KindRoundCompleted)
	})
	if got := strings.Join(eventKinds(events), ","); got !=
		"execution.started,user.input,round.started,assistant.text,round.completed,execution.finished" {
		t.Fatalf("events = %s", got)
	}
	text, _ := findPayload[event.AssistantText](events)
	if text.Text != "你好，我在" {
		t.Fatalf("assistant text = %+v", text)
	}
	started, _ := findPayload[event.RoundStarted](events)
	if started.Model != "deepseek-v4-pro" ||
		started.Dialect != event.DialectOpenAI ||
		started.ProfileVersion != "e2e-v1" {
		t.Fatalf("round.started = %+v", started)
	}
	// 一轮模型调用 + 一次判定 Idle 的空转（Idle 不调模型）
	if calls := model.calls.Load(); calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
}

/* ---------- 用例：多轮工具 ---------- */

const e2eSyncToolName = "e2e_echo"

func newE2ESyncTool(calls *atomic.Int32) tool.Spec {
	return tool.Spec{
		Name:        e2eSyncToolName,
		Description: "E2E 用同步工具：回显 text",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Binding: tool.InlineBinding{Execute: func(_ context.Context, input json.RawMessage, _ tool.Context) (any, error) {
			calls.Add(1)
			var in struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(input, &in)
			return map[string]string{"echoed": in.Text}, nil
		}},
	}
}

// TestE2EToolRounds 覆盖多轮工具：模型连调两次工具后才给答复，每次调用的结果
// 都写入事件日志并在下一轮 fold 回历史里（模型看到的消息条数逐轮增长）。
func TestE2EToolRounds(t *testing.T) {
	var toolCalls atomic.Int32
	registry, err := tool.NewRegistry(newE2ESyncTool(&toolCalls))
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{ID: "call_1", Name: e2eSyncToolName, Args: json.RawMessage(`{"text":"一"}`)}}},
		{toolCalls: []chat.ToolCall{{ID: "call_2", Name: e2eSyncToolName, Args: json.RawMessage(`{"text":"二"}`)}}},
		{text: "都回显完了"},
	}}
	env := newE2EEnv(t, e2eOptions{model: model, registry: registry})

	sessionID := env.newSession(t, "多轮工具")
	env.send(t, sessionID, "回显两次")
	if err := env.waitWorkflow(t, sessionID, 90*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}

	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return countKind(evs, event.KindRoundCompleted) == 3
	})
	if n := countKind(events, event.KindAssistantToolCall); n != 2 {
		t.Fatalf("tool calls = %d, want 2; events = %v", n, eventKinds(events))
	}
	if n := countKind(events, event.KindToolExecSucceeded); n != 2 {
		t.Fatalf("tool results = %d, want 2", n)
	}
	if n := toolCalls.Load(); n != 2 {
		t.Fatalf("tool executed %d times, want 2", n)
	}
	// 历史逐轮变长：工具结果确实回喂给了模型
	_, seen := model.snapshot()
	if len(seen) != 3 || seen[0] >= seen[1] || seen[1] >= seen[2] {
		t.Fatalf("history lengths per round = %v, want strictly increasing", seen)
	}
}

/* ---------- 用例：ActivityBinding 声明式工具 ---------- */

const (
	e2eHeavyToolName     = "e2e_heavy"
	e2eHeavyActivityName = "agentk.test.e2e_heavy"
)

// TestE2EDeclaredToolActivity 覆盖 ActivityBinding 全链路：Begin 落 started 并
// 透传 LLM 入参（模型选择在其中），capability activity 按名派发（Temporal UI
// 由此按名可见——History 里的 ActivityType 就是 capability 名），Finish 把结果
// 闭合写入事件日志。
func TestE2EDeclaredToolActivity(t *testing.T) {
	var toolCalls atomic.Int32
	var seenModel atomic.Value
	extQueue := fmt.Sprintf("agentk-external-tool-activity-%d", time.Now().UnixNano())
	registry, err := tool.NewRegistry(tool.Spec{
		Name:        e2eHeavyToolName,
		Description: "E2E 用 ActivityBinding 重工具",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.ActivityBinding{
			ActivityName:        e2eHeavyActivityName,
			TaskQueue:           extQueue,
			StartToCloseTimeout: time.Minute,
			MaxAttempts:         2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{ID: "call_h", Name: e2eHeavyToolName, Args: json.RawMessage(`{"model":"m-e2e"}`)}}},
		{text: "干完了"},
	}}
	env := newE2EEnv(t, e2eOptions{
		model: model, registry: registry,
	})
	extWorker := temporalworker.New(env.client, extQueue, temporalworker.Options{})
	extWorker.RegisterActivityWithOptions(
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			toolCalls.Add(1)
			seenModel.Store(args["model"])
			return map[string]any{"result": "heavy-ok"}, nil
		},
		activity.RegisterOptions{Name: e2eHeavyActivityName},
	)
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start external activity worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)

	sessionID := env.newSession(t, "重工具")
	env.send(t, sessionID, "来一发")
	if err := env.waitWorkflow(t, sessionID, 90*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}

	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return countKind(evs, event.KindRoundCompleted) == 2
	})
	if n := countKind(events, event.KindToolExecSucceeded); n != 1 {
		t.Fatalf("tool results = %d, want 1; events = %v", n, eventKinds(events))
	}
	if n := toolCalls.Load(); n != 1 {
		t.Fatalf("tool executed %d times, want 1", n)
	}
	// LLM 入参里的 model 必须原样到达 capability：模型选择是 LLM 的调用参数。
	if got := seenModel.Load(); got != "m-e2e" {
		t.Fatalf("capability saw model = %v, want m-e2e", got)
	}

	// History 按 capability 名可见（Temporal UI 同源）：通用 ExecuteToolActivity
	// 不该出现，记账两半按各自的名字出现。
	iter := env.client.GetWorkflowHistory(
		context.Background(), execution.WorkflowID(sessionID), "", false,
		enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var scheduled []string
	for iter.HasNext() {
		ev, err := iter.Next()
		if err != nil {
			t.Fatal(err)
		}
		if attrs := ev.GetActivityTaskScheduledEventAttributes(); attrs != nil {
			scheduled = append(scheduled, attrs.ActivityType.GetName())
		}
	}
	if !slices.Contains(scheduled, e2eHeavyActivityName) {
		t.Fatalf("history activities = %v, want %s scheduled by name", scheduled, e2eHeavyActivityName)
	}
	for _, name := range []string{agentloop.BeginToolActivityName, agentloop.FinishToolActivityName} {
		if !slices.Contains(scheduled, name) {
			t.Fatalf("history activities = %v, want %s", scheduled, name)
		}
	}
	if slices.Contains(scheduled, agentloop.ExecuteInlineToolActivityName) {
		t.Fatalf("history activities = %v: declared call must not go through the generic tool activity", scheduled)
	}
}

/* ---------- 用例：ask_user 重启模型 ---------- */

// TestE2EAskUserRestart 覆盖 restart 模型：模型提问后 workflow 就地收尾（等待
// 不占 workflow），用户作答时 API 追加事件并重开一个 workflow 接着跑。
func TestE2EAskUserRestart(t *testing.T) {
	registry, err := tool.NewRegistry(tool.NewAskUserTool())
	if err != nil {
		t.Fatal(err)
	}
	askArgs := `{"questions":[{"id":"q1","question":"画风？","options":["写实","卡通"]}]}`
	model := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{ID: "call_ask", Name: tool.AskUserToolName, Args: json.RawMessage(askArgs)}}},
		{text: "好的，卡通风"},
	}}
	env := newE2EEnv(t, e2eOptions{model: model, registry: registry})

	sessionID := env.newSession(t, "提问")
	env.send(t, sessionID, "帮我画点东西")
	if err := env.waitWorkflow(t, sessionID, 60*time.Second); err != nil {
		t.Fatalf("wait first burst: %v", err)
	}

	// 第一段：提问已登记，workflow 已收尾，模型只被调过一次。
	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return hasKind(evs, event.KindAskUserRequested)
	})
	ask, _ := findPayload[event.AskUserRequested](events)
	if ask.ToolCallID != "call_ask" || len(ask.Questions) != 1 {
		t.Fatalf("ask_user.requested = %+v", ask)
	}
	if calls := model.calls.Load(); calls != 1 {
		t.Fatalf("model calls before answer = %d, want 1", calls)
	}

	// 作答：追加事件并重开 workflow。
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+sessionID+"/answers", map[string]any{
		"toolCallId": "call_ask",
		"answers":    []map[string]any{{"questionId": "q1", "selected": []string{"卡通"}}},
	}, http.StatusAccepted, nil)
	if err := env.waitWorkflow(t, sessionID, 60*time.Second); err != nil {
		t.Fatalf("wait restarted burst: %v", err)
	}
	events = env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return countKind(evs, event.KindRoundCompleted) == 2
	})
	if !hasKind(events, event.KindAskUserResponded) {
		t.Fatalf("events = %v", eventKinds(events))
	}
	text, _ := findPayload[event.AssistantText](events)
	if text.Text != "好的，卡通风" {
		t.Fatalf("assistant text = %+v", text)
	}
}

/* ---------- 用例：WorkflowBinding ---------- */

const (
	e2eFilmToolName     = "e2e_film"
	e2eFilmWorkflowType = "agentk.test.e2e_film"
)

func newE2EFilmRegistry(t *testing.T, taskQueue string) *tool.Registry {
	t.Helper()
	registry, err := tool.NewRegistry(tool.Spec{
		Name:        e2eFilmToolName,
		Description: "E2E 用 WorkflowBinding 工具",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Binding: tool.WorkflowBinding{
			WorkflowType: e2eFilmWorkflowType,
			TaskQueue:    taskQueue,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// TestE2EWorkflowToolSync 覆盖 WorkflowBinding 同步档全链路：Begin 落 started →
// AgentWorkflow 阻塞 child 执行目标 workflow → Finish 把结果闭合写入事件日志。
func TestE2EWorkflowToolSync(t *testing.T) {
	var seenTopic atomic.Value
	extQueue := fmt.Sprintf("agentk-external-tool-workflow-sync-%d", time.Now().UnixNano())
	model := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{ID: "call_film", Name: e2eFilmToolName, Args: json.RawMessage(`{"topic":"猫"}`)}}},
		{text: "拍完了"},
	}}
	env := newE2EEnv(t, e2eOptions{
		model: model, registry: newE2EFilmRegistry(t, extQueue),
	})
	extWorker := temporalworker.New(env.client, extQueue, temporalworker.Options{})
	extWorker.RegisterWorkflowWithOptions(
		func(_ workflow.Context, args map[string]any) (json.RawMessage, error) {
			seenTopic.Store(args["topic"])
			return json.RawMessage(`{"film":"http://v/e2e.mp4"}`), nil
		},
		workflow.RegisterOptions{Name: e2eFilmWorkflowType},
	)
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start external workflow worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)

	sessionID := env.newSession(t, "同步成片")
	env.send(t, sessionID, "拍个片")
	if err := env.waitWorkflow(t, sessionID, 90*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}
	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return countKind(evs, event.KindRoundCompleted) == 2
	})
	if got := seenTopic.Load(); got != "猫" {
		t.Fatalf("child saw topic = %v", got)
	}
	ok, found := findPayload[event.ToolExecSucceeded](events)
	if !found || !strings.Contains(string(ok.Result), "e2e.mp4") {
		t.Fatalf("tool result = %+v; events = %v", ok, eventKinds(events))
	}
}

// TestE2EDynamicToolAppearsAfterWorkerAnnounce 覆盖 Profile Tool capability 的完整纵切：
// Session 可在 Worker 上报前创建，但执行会明确失败；上报后同一 Session 可按
// 会话内别名把 Activity 派发到外部队列。
func TestE2EDynamicToolAppearsAfterWorkerAnnounce(t *testing.T) {
	const (
		capabilityID = "contrib.e2e.dynamic_tool"
		activityType = "e2e.dynamic_tool.activity"
		toolName     = "dynamic_echo"
	)
	model := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{
			ID: "call_dynamic", Name: toolName, Args: json.RawMessage(`{"text":"猫"}`),
		}}},
		{text: "动态工具完成"},
	}}
	env := newE2EEnv(t, e2eOptions{model: model, dynamicProfile: true})

	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "动态工具",
		"profile": map[string]any{
			"defaultModel": "deepseek-v4-pro",
			"system":       "s",
			"tools": []map[string]any{{
				"capability": capabilityID, "name": toolName,
				"instructions": "只回显用户给出的文本",
			}},
		},
	}, http.StatusCreated, &session)

	env.send(t, session.ID, "先检查工具")
	if err := env.waitWorkflow(t, session.ID, 30*time.Second); err == nil ||
		!strings.Contains(err.Error(), "capability catalog entry not found") {
		t.Fatalf("first run error = %v, want fail-closed catalog error", err)
	}

	extQueue := fmt.Sprintf("agentk-dynamic-tool-test-%d", time.Now().UnixNano())
	var calls atomic.Int32
	extWorker := agentkworker.New(
		env.client,
		extQueue,
		temporalworker.Options{},
	)
	extWorker.RegisterActivityWithOptions(
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			calls.Add(1)
			return map[string]any{"from": "external", "text": args["text"]}, nil
		},
		activity.RegisterOptions{Name: activityType},
	)
	extWorker.PublishActivity(sdkcapability.Capability{
		ID:             capabilityID,
		TemporalType:   activityType,
		Description:    "跨队列动态回显",
		InputSchema:    json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		TimeoutSeconds: 60,
		Tool: &sdkcapability.Tool{
			Mode: sdkcapability.ToolModeBlocking,
		},
	})
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start dynamic tool worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)
	env.waitToolCapability(t, capabilityID, 10*time.Second)

	env.send(t, session.ID, "现在调用工具")
	if err := env.waitWorkflow(t, session.ID, 60*time.Second); err != nil {
		t.Fatalf("wait dynamic tool run: %v", err)
	}
	events := env.waitEvents(t, session.ID, 10*time.Second, func(events []event.Event) bool {
		return countKind(events, event.KindRoundCompleted) >= 2 &&
			hasKind(events, event.KindToolExecSucceeded)
	})
	if calls.Load() != 1 {
		t.Fatalf("external activity calls = %d, want 1", calls.Load())
	}
	result, found := findPayload[event.ToolExecSucceeded](events)
	if !found || !strings.Contains(string(result.Result), `"from":"external"`) {
		t.Fatalf("dynamic tool result = %+v", result)
	}
	var sawDynamic bool
	for _, item := range events {
		if started, ok := item.Payload.(event.RoundStarted); ok &&
			slices.Contains(started.Tools, toolName) {
			sawDynamic = true
		}
	}
	if !sawDynamic {
		t.Fatalf("no round exposed dynamic tool %q", toolName)
	}
}

func TestE2EWorkflowToolSurvivesCatalogLossAfterCall(t *testing.T) {
	const (
		capabilityID = "contrib.e2e.frozen_film"
		workflowType = "e2e.frozen_film.workflow"
		toolName     = "frozen_film"
	)
	directory := &switchableCatalog{Catalog: capcatalog.NewMemory()}
	script := &scriptedModel{turns: []scriptedTurn{
		{toolCalls: []chat.ToolCall{{
			ID: "call_frozen_film", Name: toolName,
			Args: json.RawMessage(`{"topic":"猫"}`),
		}}},
	}}
	model := &afterResultModel{
		delegate: script,
		after:    func() { directory.unavailable.Store(true) },
	}
	env := newE2EEnv(t, e2eOptions{
		model: model, dynamicProfile: true, catalog: directory,
	})

	extQueue := fmt.Sprintf("agentk-frozen-film-test-%d", time.Now().UnixNano())
	extWorker := agentkworker.New(
		env.client,
		extQueue,
		temporalworker.Options{},
	)
	extWorker.RegisterWorkflowWithOptions(
		func(_ workflow.Context, args map[string]any) (json.RawMessage, error) {
			if args["topic"] != "猫" {
				return nil, fmt.Errorf("unexpected args %v", args)
			}
			return json.RawMessage(
				`{"artifacts":[{"type":"video","uri":"http://v/frozen.mp4","description":"冻结后的成片"}]}`,
			), nil
		},
		workflow.RegisterOptions{Name: workflowType},
	)
	extWorker.PublishWorkflow(sdkcapability.Capability{
		ID:             capabilityID,
		TemporalType:   workflowType,
		Description:    "生成成片",
		InputSchema:    json.RawMessage(`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["artifacts"],"properties":{"artifacts":{"type":"array","items":{"type":"object","required":["type","uri"],"properties":{"type":{"const":"video"},"uri":{"type":"string"},"description":{"type":"string"}}}}}}`),
		TimeoutSeconds: 60,
		Tool: &sdkcapability.Tool{
			Mode: sdkcapability.ToolModeBlocking,
		},
	})
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start frozen film worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)
	env.waitToolCapability(t, capabilityID, 10*time.Second)

	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "冻结 Workflow 工具",
		"profile": map[string]any{
			"defaultModel": "deepseek-v4-pro",
			"system":       "s",
			"tools": []map[string]any{{
				"capability": capabilityID, "name": toolName,
			}},
		},
	}, http.StatusCreated, &session)
	env.send(t, session.ID, "生成一个猫咪成片")

	events := env.waitEvents(t, session.ID, 120*time.Second, func(events []event.Event) bool {
		return hasKind(events, event.KindToolExecSucceeded)
	})
	if _, err := directory.ResolveTool(
		context.Background(), capabilityID,
	); !errors.Is(err, capcatalog.ErrNotFound) {
		t.Fatalf("catalog must be unavailable after tool call, got %v", err)
	}
	completed, found := findPayload[event.ToolExecSucceeded](events)
	if !found || len(completed.Artifacts) != 1 ||
		completed.Artifacts[0].Type != "video" ||
		completed.Artifacts[0].URI != "http://v/frozen.mp4" ||
		completed.Artifacts[0].Description != "冻结后的成片" {
		t.Fatalf("tool.exec_succeeded = %+v", completed)
	}
}

/* ---------- 用例：SSE 实时旁路 ---------- */

// streamEndKind 是收流帧，与 api 侧同名（那边的常量未导出）。
const streamEndKind = "stream.end"

// TestE2EStreamLive 覆盖实时旁路的贯通：run 在途时开流，读得到执行侧写进 delta
// 缓冲的帧；run 收尾时流以收流帧关闭；之后再开只得 204——历史与最终事实走
// /events，不走这里。
func TestE2EStreamLive(t *testing.T) {
	model := &slowFirstModel{
		scriptedModel: scriptedModel{turns: []scriptedTurn{{reasoning: "先想想", text: "想好了"}}},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	env := newE2EEnv(t, e2eOptions{model: model})

	sessionID := env.newSession(t, "活流")
	env.send(t, sessionID, "说点什么")
	// begin_execution activity 先于 round activity：模型被叫到时缓冲已经 active。
	select {
	case <-model.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("first round did not start within 60s")
	}
	resp := env.openStream(t, "/v1/sessions/"+sessionID+"/stream", http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}
	close(model.release)

	frames := readSSEUntilEnd(t, resp)
	deltaFrames := 0
	for _, f := range frames[:len(frames)-1] {
		if f.kind != deltabuf.DeltaKind {
			// 已提交事件帧带 EventID，delta 帧没有——writeSSEFrame 的契约。
			if f.id == "" {
				t.Fatalf("event frame without id: %+v", f)
			}
			continue
		}
		if f.id != "" {
			t.Fatalf("delta frame carries an id: %+v", f)
		}
		var frame deltabuf.Frame
		if err := json.Unmarshal([]byte(f.data), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Text == "" {
			t.Fatalf("delta frame without text: %+v", f)
		}
		deltaFrames++
	}
	if deltaFrames == 0 {
		t.Fatalf("no delta reached the stream; kinds = %v", sseKinds(frames))
	}
	if !slices.Contains(sseKinds(frames), string(event.KindAssistantText)) {
		t.Fatalf("kinds = %v, want an assistant text event frame", sseKinds(frames))
	}

	if err := env.waitWorkflow(t, sessionID, 60*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}
	env.openStream(t, "/v1/sessions/"+sessionID+"/stream", http.StatusNoContent)
}

type sseFrame struct {
	id   string
	kind string
	data string
}

func sseKinds(frames []sseFrame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.kind)
	}
	return out
}

// openStream 开 SSE 连接并断言状态码。响应体交给调用方读，连接在测试结束时关闭。
func (e *e2eEnv) openStream(t *testing.T, path string, wantStatus int) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.httpServer.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != wantStatus {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: status = %d, want %d; body: %s", path, resp.StatusCode, wantStatus, raw)
	}
	return resp
}

// readSSEUntilEnd 读到收流帧为止。会话没有终态，流只在当前 run 收尾时关闭。
func readSSEUntilEnd(t *testing.T, resp *http.Response) []sseFrame {
	t.Helper()
	var frames []sseFrame
	var cur sseFrame
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if cur.kind != "" {
				frames = append(frames, cur)
				if cur.kind == streamEndKind {
					return frames
				}
			}
			cur = sseFrame{}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			cur.kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("stream ended after %d frames without %s: %v",
		len(frames), streamEndKind, sseKinds(frames))
	return nil
}

/* ---------- 用例：忙时拒绝 ---------- */

// slowFirstModel 第一次调用先卡一会儿，给忙时发消息留出确定窗口。
type slowFirstModel struct {
	scriptedModel
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *slowFirstModel) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	first := false
	m.once.Do(func() {
		first = true
		close(m.entered)
	})
	if first {
		select {
		case <-m.release:
		case <-ctx.Done():
			return chat.Response{}, ctx.Err()
		}
	}
	return m.scriptedModel.Stream(ctx, req)
}

// TestE2EBusySessionRejects 覆盖活跃 Agent Loop 的原子冲突判定：第二条
// 消息返回 409，既不进 Workflow History，也不进 Session 事件日志。
func TestE2EBusySessionRejects(t *testing.T) {
	model := &slowFirstModel{
		scriptedModel: scriptedModel{turns: []scriptedTurn{{text: "第一条答复"}}},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	env := newE2EEnv(t, e2eOptions{model: model})

	sessionID := env.newSession(t, "排队")
	env.send(t, sessionID, "第一条")
	select {
	case <-model.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("first round did not start within 60s")
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+sessionID+"/messages",
		map[string]string{"text": "第二条"}, http.StatusConflict, nil)
	close(model.release)

	if err := env.waitWorkflow(t, sessionID, 90*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}
	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return countKind(evs, event.KindRoundCompleted) == 1
	})
	if n := countKind(events, event.KindUserInput); n != 1 {
		t.Fatalf("user inputs = %d, want 1; events = %v", n, eventKinds(events))
	}
	if calls := model.calls.Load(); calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
}

func TestE2EAgentLoopBlocksPipeline(t *testing.T) {
	model := &slowFirstModel{
		scriptedModel: scriptedModel{turns: []scriptedTurn{{text: "done"}}},
		entered:       make(chan struct{}),
		release:       make(chan struct{}),
	}
	env := newE2EEnv(t, e2eOptions{model: model, pipelines: true})
	const (
		capabilityID = "contrib.e2e.conflict"
		pipelineName = "conflict"
	)
	if _, err := env.capabilityCatalog.Register(t.Context(), sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       "unused-conflict-queue",
		Capabilities: []sdkcapability.CapabilityDef{{
			ID: capabilityID, Kind: sdkcapability.CapabilityKindWorkflow,
			TemporalType: "unused.conflict", Description: "Conflict test",
			InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 60,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "Execution 冲突",
		"profile": map[string]any{
			"system": "", "tools": []string{},
			"pipelines": []map[string]any{{"capability": capabilityID, "name": pipelineName}},
		},
	}, http.StatusCreated, &session)
	env.send(t, session.ID, "先运行 Agent Loop")
	select {
	case <-model.entered:
	case <-time.After(60 * time.Second):
		t.Fatal("agent loop did not start")
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": pipelineName, "args": map[string]any{}},
		http.StatusConflict, nil)
	close(model.release)
	if err := env.waitWorkflow(t, session.ID, 60*time.Second); err != nil {
		t.Fatal(err)
	}
}

/* ---------- 用例：重试幂等 ---------- */

// TestE2ERoundRetryIdempotent 覆盖 Activity 重试幂等：模型首次调用失败触发重试，
// 重试用同一个幂等键（round id 由 workflow 派生，replay 不变），事件批因批唯一键
// 只落一次——事件日志中没有半轮重复的历史。
func TestE2ERoundRetryIdempotent(t *testing.T) {
	model := &scriptedModel{turns: []scriptedTurn{
		{err: fmt.Errorf("transient upstream flake")},
		{text: "重试后的答复"},
	}}
	env := newE2EEnv(t, e2eOptions{model: model})

	sessionID := env.newSession(t, "重试")
	env.send(t, sessionID, "抖一下")
	if err := env.waitWorkflow(t, sessionID, 90*time.Second); err != nil {
		t.Fatalf("wait session workflow: %v", err)
	}

	events := env.waitEvents(t, sessionID, 10*time.Second, func(evs []event.Event) bool {
		return hasKind(evs, event.KindRoundCompleted)
	})
	if got := strings.Join(eventKinds(events), ","); got !=
		"execution.started,user.input,round.started,assistant.text,round.completed,execution.finished" {
		t.Fatalf("events = %s (retry must not duplicate the round)", got)
	}
	keys, _ := model.snapshot()
	if len(keys) != 2 || keys[0] != keys[1] || keys[0] == "" {
		t.Fatalf("idempotency keys = %v, want two identical non-empty keys", keys)
	}
}

/* ---------- 用例：取消 ---------- */

// TestE2ECancel 覆盖取消链路：POST /cancel → CancelWorkflow → 在飞的
// RoundActivity 被取消 → workflow 在 disconnected context 上写 cancelled 终态。
func TestE2ECancel(t *testing.T) {
	model := &blockingModel{started: make(chan struct{})}
	env := newE2EEnv(t, e2eOptions{model: model})

	sessionID := env.newSession(t, "取消")
	env.send(t, sessionID, "慢慢想")
	select {
	case <-model.started:
	case <-time.After(60 * time.Second):
		t.Fatal("round did not start within 60s")
	}

	env.doJSON(t, http.MethodPost, "/v1/sessions/"+sessionID+"/cancel",
		nil, http.StatusAccepted, nil)

	err := env.waitWorkflow(t, sessionID, 90*time.Second)
	if err == nil || !temporal.IsCanceledError(err) {
		t.Fatalf("workflow result err = %v, want CanceledError", err)
	}
	events := env.waitEvents(t, sessionID, 15*time.Second, func(evs []event.Event) bool {
		return hasKind(evs, event.KindExecutionFinished)
	})
	finished, _ := findPayload[event.ExecutionFinished](events)
	if finished.Status != types.ExecutionStatusCancelled {
		t.Fatalf("execution.finished = %+v", finished)
	}
	if hasKind(events, event.KindRoundCompleted) {
		t.Fatalf("cancelled round must not complete; events = %v", eventKinds(events))
	}
}

/* ---------- 用例：capability discovery ---------- */

func TestE2EManifestAnnounceAndResolve(t *testing.T) {
	env := newE2EEnv(t, e2eOptions{model: &scriptedModel{}})
	coreDeadline := time.Now().Add(10 * time.Second)
	for _, capabilityID := range []string{brandad.CapabilityID} {
		for {
			resolved, err := env.capabilityCatalog.ResolveCapability(
				t.Context(), capabilityID)
			if err == nil {
				if resolved.TaskQueue != env.taskQueue {
					t.Fatalf(
						"core pipeline capability %s task queue = %q",
						capabilityID, resolved.TaskQueue)
				}
				break
			}
			if !errors.Is(err, capcatalog.ErrNotFound) || time.Now().After(coreDeadline) {
				t.Fatalf("resolve core pipeline capability %s: %v", capabilityID, err)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	brandadToolDeadline := time.Now().Add(10 * time.Second)
	for {
		resolved, err := env.capabilityCatalog.ResolveTool(
			t.Context(), brandad.CapabilityID)
		if err == nil {
			if resolved.TaskQueue != env.taskQueue ||
				resolved.Tool.Mode != sdkcapability.ToolModeBlocking {
				t.Fatalf("core brandad tool = %+v", resolved)
			}
			break
		}
		if !errors.Is(err, capcatalog.ErrNotFound) ||
			time.Now().After(brandadToolDeadline) {
			t.Fatalf("resolve core brandad tool: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	type input struct {
		Text string `json:"text"`
	}
	type output struct {
		Summary string `json:"summary"`
	}
	const (
		capabilityID = "contrib.example.e2e_summarize"
		workflowType = "example.e2e.summarize"
	)
	capabilityQueue := fmt.Sprintf("agentk-capability-test-%d", time.Now().UnixNano())
	w := agentkworker.New(
		env.client,
		capabilityQueue,
		temporalworker.Options{},
	)
	w.RegisterWorkflowWithOptions(
		func(_ workflow.Context, in input) (output, error) {
			return output{Summary: in.Text}, nil
		},
		workflow.RegisterOptions{Name: workflowType},
	)
	w.PublishWorkflow(sdkcapability.Capability{
		ID:             capabilityID,
		TemporalType:   workflowType,
		Description:    "Summarize text",
		InputSchema:    sdkcapability.SchemaOf[input](),
		OutputSchema:   sdkcapability.SchemaOf[output](),
		TimeoutSeconds: 300,
	})
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)

	deadline := time.Now().Add(10 * time.Second)
	for {
		resolved, err := env.capabilityCatalog.ResolveCapability(
			t.Context(), capabilityID)
		if err == nil {
			if resolved.TaskQueue != capabilityQueue ||
				resolved.Capability.TemporalType != workflowType {
				t.Fatalf("resolved = %+v", resolved)
			}
			break
		}
		if !errors.Is(err, capcatalog.ErrNotFound) || time.Now().After(deadline) {
			t.Fatalf("resolve announced capability: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

/* ---------- 用例：pipeline ---------- */

// TestE2EPipeline 覆盖统一声明 → container → 普通 workflow。执行体是测试
// 专属的回显 workflow（经 opts.workflows 注册），不依赖任何真实能力实现。
func TestE2EPipeline(t *testing.T) {
	const pipelineName = "e2e_echo"
	const pipelineWorkflowType = "e2e.pipeline_echo"
	const capabilityID = "contrib.e2e.pipeline_echo"
	env := newE2EEnv(t, e2eOptions{
		model: &scriptedModel{}, pipelines: true,
	})
	contribQueue := fmt.Sprintf("contrib-pipeline-test-%d", time.Now().UnixNano())
	contribWorker := temporalworker.New(
		env.client, contribQueue, temporalworker.Options{})
	contribWorker.RegisterWorkflowWithOptions(
		func(ctx workflow.Context, args map[string]any) (json.RawMessage, error) {
			for range 2 {
				if err := sdkinvocation.ReportProgress(
					ctx, "received", "Input received", json.RawMessage(`{"accepted":true}`),
				); err != nil {
					return nil, err
				}
			}
			return json.Marshal(map[string]any{"echoed": args})
		},
		workflow.RegisterOptions{Name: pipelineWorkflowType},
	)
	if err := contribWorker.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(contribWorker.Stop)

	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "pipeline",
		"profile": map[string]any{
			"system": "s", "tools": []string{},
			"pipelines": []map[string]any{{
				"capability": capabilityID, "name": pipelineName,
				"displayName": "E2E Echo",
			}},
		},
	}, http.StatusCreated, &session)

	var page struct {
		Pipelines []struct {
			CapabilityID string          `json:"capabilityId"`
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"inputSchema"`
		} `json:"pipelines"`
	}
	// Profile 可在 Worker announce 前创建；此时 List 省略该项，Start 明确 503。
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipelines", nil, http.StatusOK, &page)
	if len(page.Pipelines) != 0 {
		t.Fatalf("pipelines before announce = %+v", page.Pipelines)
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": pipelineName, "args": map[string]any{
			"brand": "pipeline e2e", "selling_point": "快",
		}}, http.StatusServiceUnavailable, nil)

	_, err := env.capabilityCatalog.Register(t.Context(), sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       contribQueue,
		Capabilities: []sdkcapability.CapabilityDef{{
			ID:             capabilityID,
			Kind:           sdkcapability.CapabilityKindWorkflow,
			TemporalType:   pipelineWorkflowType,
			Description:    "E2E echo capability",
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["brand","selling_point"],"properties":{"brand":{"type":"string"},"selling_point":{"type":"string"}}}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 300,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipelines", nil, http.StatusOK, &page)
	if len(page.Pipelines) != 1 || page.Pipelines[0].Name != pipelineName ||
		page.Pipelines[0].CapabilityID != capabilityID ||
		page.Pipelines[0].Description == "" || len(page.Pipelines[0].InputSchema) == 0 {
		t.Fatalf("pipelines = %+v", page.Pipelines)
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": pipelineName, "args": map[string]any{"brand": "缺字段"}},
		http.StatusBadRequest, nil)

	var started struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		CapabilityID string `json:"capabilityId"`
		Status       string `json:"status"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{
			"pipeline": pipelineName,
			"args":     map[string]string{"brand": "pipeline e2e", "selling_point": "快"},
		},
		http.StatusAccepted, &started)
	if started.ID == "" || started.Name != pipelineName ||
		started.CapabilityID != capabilityID ||
		started.Status != string(types.ExecutionStatusRunning) {
		t.Fatalf("started exec = %+v", started)
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": "nope"}, http.StatusNotFound, nil)
	// 未被会话暴露的 pipeline 同样 404（存在但对这个会话不可用）
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": "external_unexposed_pipeline",
			"args": map[string]string{"topic": "猫"}}, http.StatusNotFound, nil)

	if err := env.waitWorkflow(t, session.ID, 60*time.Second); err != nil {
		t.Fatalf("wait pipeline workflow: %v", err)
	}
	var got struct {
		CapabilityID string          `json:"capabilityId"`
		Status       string          `json:"status"`
		Output       json.RawMessage `json:"output"`
		FinishedAt   *time.Time      `json:"finishedAt"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipeline-runs/"+started.ID, nil, http.StatusOK, &got)
	if got.Status != string(types.ExecutionStatusCompleted) ||
		got.CapabilityID != capabilityID ||
		!strings.Contains(string(got.Output), "pipeline e2e") || got.FinishedAt == nil {
		t.Fatalf("exec = %+v", got)
	}

	// 生命周期与通用进度事件进会话事件日志。workflow 对同一步骤汇报两次，
	// invocationID + stepID 幂等键保证事件日志只有一条进度事件。
	var events struct {
		Events []struct {
			ExecutionID string          `json:"executionId"`
			Kind        string          `json:"kind"`
			Data        json.RawMessage `json:"data"`
		} `json:"events"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/events", nil, http.StatusOK, &events)
	var invocationKinds []string
	for _, ev := range events.Events {
		if ev.Kind == "execution.started" || ev.Kind == "execution.finished" || ev.Kind == "invocation.progress" {
			invocationKinds = append(invocationKinds, ev.Kind)
			if ev.ExecutionID != started.ID {
				t.Fatalf("event %s executionId = %q, want %q", ev.Kind, ev.ExecutionID, started.ID)
			}
			if ev.Kind == "execution.started" &&
				!strings.Contains(string(ev.Data), `"capabilityId":"`+capabilityID+`"`) {
				t.Fatalf("event %s does not carry capability id %s: %s",
					ev.Kind, capabilityID, ev.Data)
			}
			if ev.Kind == "invocation.progress" &&
				(!strings.Contains(string(ev.Data), `"invocationKind":"pipeline"`) ||
					!strings.Contains(string(ev.Data), `"invocationName":"`+pipelineName+`"`) ||
					!strings.Contains(string(ev.Data), `"stepId":"received"`)) {
				t.Fatalf("progress event has invalid invocation coordinates: %s", ev.Data)
			}
		}
	}
	if len(invocationKinds) != 3 ||
		invocationKinds[0] != "execution.started" ||
		invocationKinds[1] != "invocation.progress" ||
		invocationKinds[2] != "execution.finished" {
		t.Fatalf(
			"invocation event kinds = %v, want [execution.started invocation.progress execution.finished]",
			invocationKinds,
		)
	}
}

/* ---------- 用例：跨队列 pipeline 实现 ---------- */

func TestE2ECrossQueuePipeline(t *testing.T) {
	env := newE2EEnv(t, e2eOptions{model: &scriptedModel{}, pipelines: true})

	// 第二个 worker 模拟独立贡献者进程；实现只收到原始 args。
	extQueue := fmt.Sprintf("agentk-external-test-%d", time.Now().UnixNano())
	const extWorkflowType = "e2e.ext.film"
	const extCapabilityID = "contrib.e2e.external_film"
	var mu sync.Mutex
	var childSeen []map[string]any
	childWorkflow := func(_ workflow.Context, args map[string]any) (map[string]any, error) {
		mu.Lock()
		childSeen = append(childSeen, args)
		mu.Unlock()
		return map[string]any{"from": "cross-queue", "args": args}, nil
	}
	extWorker := agentkworker.New(
		env.client,
		extQueue,
		temporalworker.Options{},
	)
	extWorker.RegisterWorkflowWithOptions(childWorkflow, workflow.RegisterOptions{Name: extWorkflowType})
	extWorker.PublishWorkflow(sdkcapability.Capability{
		ID:             extCapabilityID,
		TemporalType:   extWorkflowType,
		Description:    "Cross-queue film",
		InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["topic"],"properties":{"topic":{"type":"string"}}}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		TimeoutSeconds: 300,
	})
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start external worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := env.capabilityCatalog.ResolveCapability(
			t.Context(), extCapabilityID)
		if err == nil {
			break
		}
		if !errors.Is(err, capcatalog.ErrNotFound) || time.Now().After(deadline) {
			t.Fatalf("resolve external pipeline: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}

	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "外部 pipeline",
		"profile": map[string]any{
			"system": "s", "tools": []string{},
			"pipelines": []map[string]any{{
				"capability": extCapabilityID,
				"name":       "e2e_cross_queue",
			}},
		},
	}, http.StatusCreated, &session)

	var page struct {
		Pipelines []struct {
			Name string `json:"name"`
		} `json:"pipelines"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipelines", nil, http.StatusOK, &page)
	if len(page.Pipelines) != 1 || page.Pipelines[0].Name != "e2e_cross_queue" {
		t.Fatalf("pipelines = %+v", page.Pipelines)
	}

	var started struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/pipeline-runs",
		map[string]any{"pipeline": "e2e_cross_queue", "args": map[string]string{"topic": "猫"}},
		http.StatusAccepted, &started)
	if started.ID == "" || started.Name != "e2e_cross_queue" ||
		started.Status != string(types.ExecutionStatusRunning) {
		t.Fatalf("started exec = %+v", started)
	}

	if err := env.waitWorkflow(t, session.ID, 60*time.Second); err != nil {
		t.Fatalf("wait container workflow: %v", err)
	}
	var got struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipeline-runs/"+started.ID, nil, http.StatusOK, &got)
	if got.Status != string(types.ExecutionStatusCompleted) ||
		!strings.Contains(string(got.Output), `"from":"cross-queue"`) ||
		!strings.Contains(string(got.Output), "猫") {
		t.Fatalf("exec = %+v output = %s", got, got.Output)
	}

	// child 只收到 args，不存在 agentk session/tenant/exec 字段。
	mu.Lock()
	seen := append([]map[string]any(nil), childSeen...)
	mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("child executions = %d, want 1", len(seen))
	}
	if seen[0]["topic"] != "猫" || seen[0]["session"] != nil ||
		seen[0]["tenant"] != nil || seen[0]["execPublicId"] != nil {
		t.Fatalf("child input = %+v", seen[0])
	}

	// 实现没有 AgentK 记账坐标，事件日志只有统一 Execution 生命周期。
	var events struct {
		Events []struct {
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		} `json:"events"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/events", nil, http.StatusOK, &events)
	var pipelineKinds []string
	for _, ev := range events.Events {
		if ev.Kind == "execution.started" || ev.Kind == "execution.finished" {
			pipelineKinds = append(pipelineKinds, ev.Kind)
		}
	}
	if got, want := strings.Join(pipelineKinds, ","),
		"execution.started,execution.finished"; got != want {
		t.Fatalf("pipeline event kinds = %s, want %s", got, want)
	}
}

/* ---------- 用例：外部 Worker 停机与恢复 ---------- */

// TestE2EExternalPipelineWorkerRecovery Worker 离线不等于能力消失。Catalog 记录
// 在 TTL 内继续有效，Start 照常受理，child 在 Temporal 队列上排着；Worker 换个
// 进程回来就把积压的任务接着跑完。这正是"Worker 在线"与"契约存在"两个事实分开
// 的意义——离线期间既不该拒单，也不该丢单。
func TestE2EExternalPipelineWorkerRecovery(t *testing.T) {
	env := newE2EEnv(t, e2eOptions{model: &scriptedModel{}, pipelines: true})

	extQueue := fmt.Sprintf("agentk-recovery-test-%d", time.Now().UnixNano())
	const (
		extWorkflowType = "e2e.recovery.film"
		extCapabilityID = "contrib.e2e.recovery_film"
		pipelineName    = "e2e_recovery"
	)
	implementation := func(_ workflow.Context, args map[string]any) (map[string]any, error) {
		return map[string]any{"from": "recovered", "topic": args["topic"]}, nil
	}
	newWorker := func() *agentkworker.Worker {
		w := agentkworker.New(env.client, extQueue, temporalworker.Options{})
		w.RegisterWorkflowWithOptions(implementation,
			workflow.RegisterOptions{Name: extWorkflowType})
		w.PublishWorkflow(sdkcapability.Capability{
			ID:             extCapabilityID,
			TemporalType:   extWorkflowType,
			Description:    "Recovery film",
			InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["topic"],"properties":{"topic":{"type":"string"}}}`),
			OutputSchema:   json.RawMessage(`{"type":"object"}`),
			TimeoutSeconds: 300,
		})
		return w
	}

	first := newWorker()
	if err := first.Start(); err != nil {
		t.Fatalf("start external worker: %v", err)
	}
	env.waitPipelineCapability(t, extCapabilityID, 10*time.Second)
	first.Stop()

	var session struct {
		ID string `json:"id"`
	}
	env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
		"title": "worker 恢复",
		"profile": map[string]any{
			"system": "s", "tools": []string{},
			"pipelines": []map[string]any{{
				"capability": extCapabilityID, "name": pipelineName,
			}},
		},
	}, http.StatusCreated, &session)

	// Worker 已停，能力仍在 Catalog（TTL 一小时），List/Start 都不受影响。
	var page struct {
		Pipelines []struct {
			Name string `json:"name"`
		} `json:"pipelines"`
	}
	env.doJSON(t, http.MethodGet, "/v1/sessions/"+session.ID+"/pipelines",
		nil, http.StatusOK, &page)
	if len(page.Pipelines) != 1 || page.Pipelines[0].Name != pipelineName {
		t.Fatalf("pipelines while worker is down = %+v", page.Pipelines)
	}
	execID := env.startPipelineRun(t, session.ID, pipelineName,
		map[string]string{"topic": "停机"})

	// Execution 已登记，但 extQueue 上没有 poller，child 一定还没跑完。
	env.waitEvents(t, session.ID, 10*time.Second, func(events []event.Event) bool {
		return hasKind(events, event.KindExecutionStarted)
	})
	desc, err := env.client.DescribeWorkflowExecution(
		t.Context(), execution.WorkflowID(session.ID), "")
	if err != nil {
		t.Fatalf("describe container: %v", err)
	}
	if status := desc.WorkflowExecutionInfo.Status; status != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		t.Fatalf("container status while worker is down = %v, want RUNNING", status)
	}
	if status, _ := env.pipelineRunOutput(t, session.ID, execID); status != string(types.ExecutionStatusRunning) {
		t.Fatalf("exec status while worker is down = %q", status)
	}
	// 同一个 Session 的 Pipeline 占用统一 Execution slot，Agent Loop 也必须被拒绝。
	env.doJSON(t, http.MethodPost, "/v1/sessions/"+session.ID+"/messages",
		map[string]string{"text": "不能并行"}, http.StatusConflict, nil)

	second := newWorker()
	if err := second.Start(); err != nil {
		t.Fatalf("restart external worker: %v", err)
	}
	t.Cleanup(second.Stop)

	if err := env.waitWorkflow(t, session.ID, 60*time.Second); err != nil {
		t.Fatalf("wait container after worker recovery: %v", err)
	}
	status, output := env.pipelineRunOutput(t, session.ID, execID)
	if status != string(types.ExecutionStatusCompleted) ||
		!strings.Contains(output, `"from":"recovered"`) ||
		!strings.Contains(output, "停机") {
		t.Fatalf("exec after recovery: status = %q output = %s", status, output)
	}

	events := env.waitEvents(t, session.ID, 10*time.Second, func(events []event.Event) bool {
		return hasKind(events, event.KindExecutionFinished)
	})
	var pipelineKinds []string
	for _, ev := range events {
		if ev.Payload.Kind() == event.KindExecutionStarted ||
			ev.Payload.Kind() == event.KindExecutionFinished {
			pipelineKinds = append(pipelineKinds, string(ev.Payload.Kind()))
		}
	}
	if got, want := strings.Join(pipelineKinds, ","),
		"execution.started,execution.finished"; got != want {
		t.Fatalf("pipeline event kinds = %s, want %s", got, want)
	}
}

/* ---------- 用例：Manifest 更新不影响在途执行 ---------- */

// TestE2EPipelineFreezesTargetAcrossManifestUpdate Target 在 Start 时从 Catalog
// 解析一次并冻进 Temporal History，之后 Catalog 怎么变都与在途执行无关。否则
// Worker 换版会让已经受理的执行改道到新实现，破坏 Temporal 的确定性重放。
func TestE2EPipelineFreezesTargetAcrossManifestUpdate(t *testing.T) {
	env := newE2EEnv(t, e2eOptions{model: &scriptedModel{}, pipelines: true})

	extQueue := fmt.Sprintf("agentk-freeze-test-%d", time.Now().UnixNano())
	const (
		capabilityID = "contrib.e2e.freeze_film"
		pipelineName = "e2e_freeze"
		workflowV1   = "e2e.freeze.film.v1"
		workflowV2   = "e2e.freeze.film.v2"
	)
	announce := func(temporalType string) {
		t.Helper()
		if _, err := env.capabilityCatalog.Register(t.Context(), sdkcapability.ManifestV1{
			ProtocolVersion: sdkcapability.ManifestProtocolVersion,
			TaskQueue:       extQueue,
			Capabilities: []sdkcapability.CapabilityDef{{
				ID:             capabilityID,
				Kind:           sdkcapability.CapabilityKindWorkflow,
				TemporalType:   temporalType,
				Description:    "Freeze film",
				InputSchema:    json.RawMessage(`{"type":"object","additionalProperties":false,"required":["topic"],"properties":{"topic":{"type":"string"}}}`),
				OutputSchema:   json.RawMessage(`{"type":"object"}`),
				TimeoutSeconds: 300,
			}},
		}); err != nil {
			t.Fatalf("announce %s: %v", temporalType, err)
		}
	}

	var session struct {
		ID string `json:"id"`
	}
	newSession := func(title string) string {
		t.Helper()
		env.doJSON(t, http.MethodPost, "/v1/sessions", map[string]any{
			"title": title,
			"profile": map[string]any{
				"system": "s", "tools": []string{},
				"pipelines": []map[string]any{{
					"capability": capabilityID, "name": pipelineName,
				}},
			},
		}, http.StatusCreated, &session)
		return session.ID
	}
	sessionV1 := newSession("Manifest V1")
	sessionV2 := newSession("Manifest V2")

	// 两个 Session 分别在 Manifest 更新前后启动；同一 Session 同时只允许一个 Execution。
	// extQueue 上还没有 Worker，因此两个 child 都会等待后续 Worker 接管。
	announce(workflowV1)
	runV1 := env.startPipelineRun(t, sessionV1, pipelineName,
		map[string]string{"topic": "旧版"})
	env.waitEvents(t, sessionV1, 10*time.Second, func(events []event.Event) bool {
		return countKind(events, event.KindExecutionStarted) == 1
	})

	announce(workflowV2)
	runV2 := env.startPipelineRun(t, sessionV2, pipelineName,
		map[string]string{"topic": "新版"})
	env.waitEvents(t, sessionV2, 10*time.Second, func(events []event.Event) bool {
		return countKind(events, event.KindExecutionStarted) == 1
	})

	// 同一个 Worker 同时提供两个版本的实现，谁被调用完全取决于冻结的 Target。
	extWorker := temporalworker.New(env.client, extQueue, temporalworker.Options{})
	for name, version := range map[string]string{workflowV1: "v1", workflowV2: "v2"} {
		extWorker.RegisterWorkflowWithOptions(
			func(_ workflow.Context, args map[string]any) (map[string]any, error) {
				return map[string]any{"version": version, "topic": args["topic"]}, nil
			},
			workflow.RegisterOptions{Name: name},
		)
	}
	if err := extWorker.Start(); err != nil {
		t.Fatalf("start external worker: %v", err)
	}
	t.Cleanup(extWorker.Stop)

	for _, tc := range []struct {
		sessionID   string
		execID      string
		wantVersion string
		wantTopic   string
	}{
		{sessionV1, runV1, "v1", "旧版"},
		{sessionV2, runV2, "v2", "新版"},
	} {
		if err := env.waitWorkflow(t, tc.sessionID, 60*time.Second); err != nil {
			t.Fatalf("wait container %s: %v", tc.execID, err)
		}
		status, output := env.pipelineRunOutput(t, tc.sessionID, tc.execID)
		if status != string(types.ExecutionStatusCompleted) ||
			!strings.Contains(output, `"version":"`+tc.wantVersion+`"`) ||
			!strings.Contains(output, tc.wantTopic) {
			t.Fatalf("exec %s: status = %q output = %s", tc.execID, status, output)
		}
	}
}
