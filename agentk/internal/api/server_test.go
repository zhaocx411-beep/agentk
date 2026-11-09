package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/service"
	"agentk/internal/store/memory"
	"agentk/internal/types"
)

/* ---------- 测试基建 ---------- */

// 内存存储与 MySQL 实现跑同一套契约测试（internal/store/storetest），
// 所以接入层测试用它就够：这里验的是路由、租户解析与 DTO 映射，不是 SQL。
var _ service.Store = (*memory.Store)(nil)

// fakeAgentLoop 是注入 service 的 Agent Loop 桩。它同步落库以便接入层断言输入翻译，
// Temporal 启动和 Activity 幂等由 agentloop 自己的测试覆盖。
type fakeAgentLoop struct {
	store     *memory.Store
	batch     int
	active    bool
	cancels   int
	failStart bool
}

func (d *fakeAgentLoop) append(
	ctx context.Context, sess types.Session, payload event.Payload,
) (types.ExecutionID, error) {
	if d.failStart {
		return "", fmt.Errorf("fake agent loop: start rejected")
	}
	if d.active {
		return "", types.ErrExecutionActive
	}
	executionID := types.NewExecutionID()
	d.batch++
	_, err := d.store.AppendEvents(ctx, sess.ID, executionID,
		event.BatchID(fmt.Sprintf("bat_%d", d.batch)),
		[]event.Draft{{Payload: payload}})
	if err == nil {
		d.active = true
	}
	return executionID, err
}

func (d *fakeAgentLoop) StartMessage(ctx context.Context, sess types.Session, text string) (types.ExecutionID, error) {
	return d.append(ctx, sess, event.UserInput{Text: text})
}

func (d *fakeAgentLoop) StartAnswer(
	ctx context.Context, sess types.Session, answer event.AskUserResponded,
) (types.ExecutionID, error) {
	return d.append(ctx, sess, answer)
}

func (d *fakeAgentLoop) Cancel(ctx context.Context, sess types.Session) error {
	if !d.active {
		return types.ErrExecutionIdle
	}
	d.cancels++
	d.active = false
	return nil
}

// fakeRuntime 恒校验通过（内容含 "bad"/"stale" 除外），不携带真实模型：
// 执行不在接入层测试范围内，这里只服务创建会话时的固化与发消息前的复核。
type fakeRuntime struct{}

// testProfile 是建会话用的最小合法 Profile。服务端没有缺省文档，每次建会话
// 都得自带一份。
const testProfile = `{"system":"测试","tools":[]}`

func (r fakeRuntime) MaterializeSessionProfile(requestJSON json.RawMessage) (json.RawMessage, error) {
	raw := strings.TrimSpace(string(requestJSON))
	// 与生产实现一致：服务端没有可继承的缺省文档（见 profile.Runtime）
	if raw == "" || raw == "null" {
		return nil, fmt.Errorf("session profile is required")
	}
	if !json.Valid(requestJSON) || !strings.HasPrefix(raw, "{") {
		return nil, fmt.Errorf("profile must be a JSON object")
	}
	if strings.Contains(raw, "bad") {
		return nil, fmt.Errorf("override profile: rejected")
	}
	return append(json.RawMessage(nil), requestJSON...), nil
}

func (r fakeRuntime) ValidateSessionProfile(profileJSON []byte) error {
	if strings.Contains(string(profileJSON), "stale") {
		return fmt.Errorf("model no longer in catalog")
	}
	return nil
}

func testRuntime(t *testing.T) fakeRuntime {
	t.Helper()
	return fakeRuntime{}
}

// testHarness 是一套装配好的接入层：内存存储 + delta 缓冲 + Agent Loop 桩。
type testHarness struct {
	url       string
	store     *memory.Store
	deltas    *deltabuf.Memory
	agentLoop *fakeAgentLoop
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	st := memory.New()
	deltas := deltabuf.NewMemory()
	agentLoop := &fakeAgentLoop{store: st}
	// handler 外包 dev principal：与生产装配（authz.Middleware 或 WithDevPrincipal）对齐
	appService := service.New(service.Config{
		Store: st, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
	})
	srv := httptest.NewServer(authz.WithDevPrincipal(
		NewServer(ServerConfig{Service: appService, Deltas: deltas}).Handler()))
	t.Cleanup(srv.Close)
	return &testHarness{url: srv.URL, store: st, deltas: deltas, agentLoop: agentLoop}
}

// newSession 建一个会话并返回它的 DTO。
func (h *testHarness) newSession(t *testing.T, title string) sessionDTO {
	t.Helper()
	var sess sessionDTO
	doJSON(t, "POST", h.url+"/v1/sessions",
		map[string]any{"title": title, "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sess)
	return sess
}

// seed 绕过 HTTP 直接往事件日志追加事件（构造模型侧才会产生的事实）。
func (h *testHarness) seed(t *testing.T, sess sessionDTO, payloads ...event.Payload) {
	t.Helper()
	stored, err := h.store.GetSessionByPublicID(context.Background(), "org_dev", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	drafts := make([]event.Draft, 0, len(payloads))
	for _, p := range payloads {
		drafts = append(drafts, event.Draft{RoundID: "rnd_seed", Payload: p})
	}
	h.agentLoop.batch++
	if _, err := h.store.AppendEvents(context.Background(), stored.ID, types.NewExecutionID(),
		event.BatchID(fmt.Sprintf("seed_%d", h.agentLoop.batch)), drafts); err != nil {
		t.Fatal(err)
	}
}

/* ---------- HTTP helpers ---------- */

func doJSON(t *testing.T, method, url string, body any, wantStatus int, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status = %d, want %d; body: %s", method, url, resp.StatusCode, wantStatus, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode response %s: %v", raw, err)
		}
	}
}

// eventsPage 是 GET /events 的响应形状。事件按 event 包的 wire 信封解码——
// 与客户端拿到的字节一模一样，顺带验一遍判别 codec 的往返。
type eventsPage struct {
	Events []event.Event `json:"events"`
}

func (h *testHarness) events(t *testing.T, sessionID string, query string) eventsPage {
	t.Helper()
	var page eventsPage
	doJSON(t, "GET", h.url+"/v1/sessions/"+sessionID+"/events"+query, nil, http.StatusOK, &page)
	return page
}

func kinds(events []event.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, string(ev.Payload.Kind()))
	}
	return out
}

/* ---------- 用例 ---------- */

// /v1/me 只回显 context principal、不触存储——直接调 handler。
func TestWhoAmI(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/me", nil)
	req = req.WithContext(authz.WithPrincipal(req.Context(), authz.Principal{
		UserPub: "sp_eval", TenantID: "org_eval", Display: "evaluation",
	}))
	rec := httptest.NewRecorder()
	(&Server{}).handleWhoAmI(rec, req)

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || got["id"] != "sp_eval" ||
		got["tenantId"] != "org_eval" || got["display"] != "evaluation" {
		t.Fatalf("status = %d, body = %v", rec.Code, got)
	}
}

// TestPostMessageStartsAgentLoop 接入层只回执 Workflow 已接受；输入落库由
// Agent Loop 的首个 Activity 负责。fake 同步模拟这一步，便于核对 DTO 翻译。
func TestPostMessageStartsAgentLoop(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "闭环测试")
	if sess.ID == "" || sess.Title != "闭环测试" {
		t.Fatalf("session = %+v", sess)
	}

	var accepted struct {
		SessionID string `json:"sessionId"`
	}
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "你好"}, http.StatusAccepted, &accepted)
	if accepted.SessionID != sess.ID {
		t.Fatalf("accepted = %+v", accepted)
	}
	page := h.events(t, sess.ID, "")
	if len(page.Events) != 1 {
		t.Fatalf("events = %+v", page.Events)
	}
	got := page.Events[0]
	payload, ok := got.Payload.(event.UserInput)
	if !ok || got.ID == "" || got.ExecutionID == "" {
		t.Fatalf("event = %+v", got)
	}
	if payload.Text != "你好" {
		t.Fatalf("payload = %+v", payload)
	}

	// 忙时不排队，也不落库。
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "再补一句"}, http.StatusConflict, nil)
	page = h.events(t, sess.ID, "")
	if len(page.Events) != 1 {
		t.Fatalf("events = %+v", page.Events)
	}
}

func TestPostMessageRejectsMultipart(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "附件测试")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "report.doc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "doc bytes"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost,
		h.url+"/v1/sessions/"+sess.ID+"/messages", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 415: %s", resp.StatusCode, raw)
	}
}

// TestListEvents 返回 Session 的完整事件日志；内部数据库 id 不进入响应。
func TestListEvents(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "分页")
	h.seed(t, sess,
		event.UserInput{Text: "画只猫"},
		event.RoundStarted{Model: "main", Dialect: event.DialectOpenAI},
		event.AssistantText{Block: 0, Text: "好的"},
		event.RoundCompleted{FinishReason: "stop"},
	)

	all := h.events(t, sess.ID, "")
	if got := kinds(all.Events); strings.Join(got, ",") !=
		"user.input,round.started,assistant.text,round.completed" {
		t.Fatalf("kinds = %v", got)
	}
	// roundId 随事件回带，eventId/executionId 是公开稳定标识。
	if all.Events[1].RoundID != "rnd_seed" {
		t.Fatalf("roundId = %q", all.Events[1].RoundID)
	}
	for _, ev := range all.Events {
		if ev.ID == "" || ev.ExecutionID == "" {
			t.Fatalf("event identifiers = %+v", ev)
		}
	}
}

// TestAnswerAskUser 回答提问 = 追加 ask_user.responded；题面由服务端从提问事件
// 回填，客户端只提供选中项。
func TestAnswerAskUser(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "提问")
	h.seed(t, sess,
		event.UserInput{Text: "帮我画点东西"},
		event.AssistantToolCall{ToolCallID: "call_1", ToolName: "ask_user", Args: event.RawJSON(`{}`)},
		event.AskUserRequested{ToolCallID: "call_1", Questions: []event.Question{
			{ID: "q1", Question: "画风？", Options: []string{"写实", "卡通"}},
		}},
	)

	var accepted struct {
		SessionID string `json:"sessionId"`
	}
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/answers", map[string]any{
		"toolCallId": "call_1",
		// 客户端给的题面被忽略：这条要喂回模型，题面必须是提问时的原文
		"answers": []map[string]any{{"questionId": "q1", "question": "伪造的题面", "selected": []string{"卡通"}}},
	}, http.StatusAccepted, &accepted)

	if accepted.SessionID != sess.ID {
		t.Fatalf("accepted = %+v", accepted)
	}
	page := h.events(t, sess.ID, "")
	if len(page.Events) != 4 {
		t.Fatalf("events = %+v", page.Events)
	}
	payload, ok := page.Events[3].Payload.(event.AskUserResponded)
	if !ok {
		t.Fatalf("payload = %T, want ask_user.responded", page.Events[3].Payload)
	}
	if payload.ToolCallID != "call_1" || len(payload.Answers) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if a := payload.Answers[0]; a.Question != "画风？" || strings.Join(a.Selected, ",") != "卡通" {
		t.Fatalf("answer = %+v", a)
	}

	// 同一张卡片再点一次：已作答，不能再补一条 tool result
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/answers", map[string]any{
		"toolCallId": "call_1",
		"answers":    []map[string]any{{"questionId": "q1", "selected": []string{"写实"}}},
	}, http.StatusConflict, nil)
}

// TestAnswerAskUserSupersededByMessage 用户没点卡片而是直接说了别的：那一轮已被
// 接管，迟到的作答会插到用户消息之后，破坏"结果紧跟调用"，必须 409。
func TestAnswerAskUserSupersededByMessage(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "接管")
	h.seed(t, sess,
		event.AskUserRequested{ToolCallID: "call_1", Questions: []event.Question{
			{ID: "q1", Question: "画风？"},
		}},
	)
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "算了，直接做"}, http.StatusAccepted, nil)

	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/answers", map[string]any{
		"toolCallId": "call_1",
		"answers":    []map[string]any{{"questionId": "q1", "selected": []string{"写实"}}},
	}, http.StatusConflict, nil)
}

// TestAnswerValidation 作答的入参校验：未知题号、空选中、单选给多个都要 400。
func TestAnswerValidation(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "校验")
	h.seed(t, sess, event.AskUserRequested{ToolCallID: "call_1", Questions: []event.Question{
		{ID: "q1", Question: "画风？"},
	}})

	answer := func(body map[string]any, want int) {
		t.Helper()
		doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/answers", body, want, nil)
	}
	answer(map[string]any{"answers": []map[string]any{{"questionId": "q1", "selected": []string{"x"}}}},
		http.StatusBadRequest) // 缺 toolCallId
	answer(map[string]any{"toolCallId": "call_1"}, http.StatusBadRequest)
	answer(map[string]any{"toolCallId": "call_1",
		"answers": []map[string]any{{"questionId": "q9", "selected": []string{"x"}}}}, http.StatusBadRequest)
	answer(map[string]any{"toolCallId": "call_1",
		"answers": []map[string]any{{"questionId": "q1", "selected": []string{}}}}, http.StatusBadRequest)
	answer(map[string]any{"toolCallId": "call_1",
		"answers": []map[string]any{{"questionId": "q1", "selected": []string{"a", "b"}}}}, http.StatusBadRequest)
	// 未提过的 tool call → 409（不是 400：请求本身合法，只是这一轮不在等它）
	answer(map[string]any{"toolCallId": "call_9",
		"answers": []map[string]any{{"questionId": "q1", "selected": []string{"x"}}}}, http.StatusConflict)
}

// TestCancelSession 取消当前活跃 Execution；空闲取消返回 409。
func TestCancelSession(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "取消")
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "画只猫"}, http.StatusAccepted, nil)

	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/cancel",
		nil, http.StatusAccepted, nil)
	if h.agentLoop.cancels != 1 {
		t.Fatalf("cancels = %d", h.agentLoop.cancels)
	}
	page := h.events(t, sess.ID, "")
	if got := kinds(page.Events); strings.Join(got, ",") != "user.input" {
		t.Fatalf("kinds = %v", got)
	}

	// 已经没有活跃 Execution，不能凭空追加事件。
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/cancel", nil, http.StatusConflict, nil)
}

// TestCreateSessionProfile profile 是必填的行为定义：非法文档一律 400，
// 超 1MB → 413（KB 量级文档的防滥用卫生线）。
func TestCreateSessionProfile(t *testing.T) {
	h := newHarness(t)
	doJSON(t, "POST", h.url+"/v1/sessions",
		map[string]any{"profile": json.RawMessage(`{"system":"bad"}`)}, http.StatusBadRequest, nil)
	doJSON(t, "POST", h.url+"/v1/sessions",
		map[string]any{"profile": strings.Repeat("x", maxSessionBody+1)},
		http.StatusRequestEntityTooLarge, nil)
}

// TestCreateSessionRequiresProfile 服务端没有实例缺省、没有兜底文档：省略
// profile、传 null、连 body 都不带，一律 400。
func TestCreateSessionRequiresProfile(t *testing.T) {
	h := newHarness(t)
	doJSON(t, "POST", h.url+"/v1/sessions",
		map[string]any{"title": "无 profile"}, http.StatusBadRequest, nil)
	doJSON(t, "POST", h.url+"/v1/sessions",
		map[string]any{"title": "null profile", "profile": nil}, http.StatusBadRequest, nil)
	doJSON(t, "POST", h.url+"/v1/sessions", nil, http.StatusBadRequest, nil)
}

// TestListSessionsKeyset 会话列表创建序倒排、keyset 翻页。
func TestListSessionsKeyset(t *testing.T) {
	h := newHarness(t)
	first := h.newSession(t, "一")
	second := h.newSession(t, "二")

	var page struct {
		Sessions []sessionDTO `json:"sessions"`
		HasMore  bool         `json:"hasMore"`
	}
	doJSON(t, "GET", h.url+"/v1/sessions?limit=1", nil, http.StatusOK, &page)
	if len(page.Sessions) != 1 || page.Sessions[0].ID != second.ID || !page.HasMore {
		t.Fatalf("page = %+v (hasMore=%v)", page.Sessions, page.HasMore)
	}
	doJSON(t, "GET", h.url+"/v1/sessions?before="+second.ID, nil, http.StatusOK, &page)
	if len(page.Sessions) != 1 || page.Sessions[0].ID != first.ID {
		t.Fatalf("second page = %+v", page.Sessions)
	}
}

func TestNotFound(t *testing.T) {
	h := newHarness(t)
	doJSON(t, "POST", h.url+"/v1/sessions/ses_nonexistent/messages",
		map[string]string{"text": "hi"}, http.StatusNotFound, nil)
	doJSON(t, "GET", h.url+"/v1/sessions/ses_nonexistent/events", nil, http.StatusNotFound, nil)
	doJSON(t, "GET", h.url+"/v1/sessions/ses_nonexistent/stream", nil, http.StatusNotFound, nil)
	doJSON(t, "POST", h.url+"/v1/sessions/ses_nonexistent/cancel", nil, http.StatusNotFound, nil)
}

func TestBadRequest(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "参数")
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": ""}, http.StatusBadRequest, nil)
	doJSON(t, "GET", h.url+"/v1/sessions/"+sess.ID+"/events?after=abc", nil, http.StatusOK, nil)
	doJSON(t, "GET", h.url+"/v1/sessions/"+sess.ID+"/events?limit=0", nil, http.StatusOK, nil)
}

// TestAgentLoopFailure 推进入口不可用（Temporal 挂了）时发消息 502——
// 不能装作收下了：事件日志中没有这条消息，客户端会永远等不到回应。
func TestAgentLoopFailure(t *testing.T) {
	h := newHarness(t)
	sess := h.newSession(t, "故障")
	h.agentLoop.failStart = true
	doJSON(t, "POST", h.url+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "你好"}, http.StatusBadGateway, nil)
}
