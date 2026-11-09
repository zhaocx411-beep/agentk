package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/event"
	"agentk/internal/service"
	"agentk/internal/store/memory"
)

// testServerMultiTenant 起一个按 X-Test-Tenant 头注入服务 principal 的测试服务：
// 模拟两个平台 Service Principal 各自绑定 org，专测跨租户隔离。
func testServerMultiTenant(t *testing.T) *httptest.Server {
	t.Helper()
	st := memory.New()
	agentLoop := &fakeAgentLoop{store: st}
	appService := service.New(service.Config{
		Store: st, AgentLoop: agentLoop, Executions: agentLoop, ProfileRuntime: testRuntime(t),
	})
	core := NewServer(ServerConfig{
		Service: appService,
		Deltas:  deltabuf.NewMemory(),
	}).Handler()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := authz.Principal{
			UserPub:  "sp_" + r.Header.Get("X-Test-Tenant"),
			TenantID: r.Header.Get("X-Test-Tenant"),
		}
		core.ServeHTTP(w, r.WithContext(authz.WithPrincipal(r.Context(), p)))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// doJSONAs 同 doJSON，带租户头。
func doJSONAs(t *testing.T, tenant, method, url string, body any, wantStatus int, out any) {
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Tenant", tenant)
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
			t.Fatalf("unmarshal response: %v; body: %s", err, raw)
		}
	}
}

// TestCrossTenantIsolation：A 建会话发消息后，B 访问 A 的全部端点一律 404；
// A 自己访问一切正常。404 不区分"不存在"与"不属于你"。
func TestCrossTenantIsolation(t *testing.T) {
	srv := testServerMultiTenant(t)

	var sess sessionDTO
	doJSONAs(t, "org_a", "POST", srv.URL+"/v1/sessions",
		map[string]any{"title": "A 的会话", "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sess)
	if sess.TenantID != "org_a" {
		t.Fatalf("session tenant = %q, want org_a（归属由身份决定）", sess.TenantID)
	}
	doJSONAs(t, "org_a", "POST", srv.URL+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "你好"}, http.StatusAccepted, nil)

	// B 访问 A 的会话：每个端点都 404
	doJSONAs(t, "org_b", "GET", srv.URL+"/v1/sessions/"+sess.ID+"/events", nil, http.StatusNotFound, nil)
	doJSONAs(t, "org_b", "GET", srv.URL+"/v1/sessions/"+sess.ID+"/stream", nil, http.StatusNotFound, nil)
	doJSONAs(t, "org_b", "POST", srv.URL+"/v1/sessions/"+sess.ID+"/messages",
		map[string]string{"text": "偷看"}, http.StatusNotFound, nil)
	doJSONAs(t, "org_b", "POST", srv.URL+"/v1/sessions/"+sess.ID+"/answers",
		map[string]any{"toolCallId": "call_1"}, http.StatusNotFound, nil)
	doJSONAs(t, "org_b", "POST", srv.URL+"/v1/sessions/"+sess.ID+"/cancel", nil, http.StatusNotFound, nil)

	// body 带别人的 tenantId 也改变不了归属
	var sessB sessionDTO
	doJSONAs(t, "org_b", "POST", srv.URL+"/v1/sessions",
		map[string]any{"tenantId": "org_a", "title": "B 伪造归属", "profile": json.RawMessage(testProfile)},
		http.StatusCreated, &sessB)
	if sessB.TenantID != "org_b" {
		t.Fatalf("session tenant = %q, want org_b（body tenantId 必须被忽略）", sessB.TenantID)
	}
	// A 也看不见 B 的会话
	doJSONAs(t, "org_a", "GET", srv.URL+"/v1/sessions/"+sessB.ID+"/events", nil, http.StatusNotFound, nil)

	// A 自己的会话一切正常
	var page eventsPage
	doJSONAs(t, "org_a", "GET", srv.URL+"/v1/sessions/"+sess.ID+"/events", nil, http.StatusOK, &page)
	if len(page.Events) != 1 || page.Events[0].Payload.Kind() != event.KindUserInput {
		t.Fatalf("A events = %+v", page.Events)
	}
}
