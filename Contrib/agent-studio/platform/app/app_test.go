package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	comicdrama "studio/agents/comic-drama"
	"studio/platform/chat"
	"studio/platform/core"
	"studio/platform/core/coretest"
)

func newAppServer(t *testing.T) *httptest.Server {
	t.Helper()
	fake := coretest.NewFakeAgentK()
	agents := chat.DefaultRegistry()
	drama := comicdrama.NewServer(
		core.ModeAgent,
		fake,
		comicdrama.NewMemoryDramaStore(),
		json.RawMessage(`{"system":"漫剧"}`),
		core.ModelRefs{},
	)
	chatServer := chat.NewServer(fake, agents, nil)
	server := httptest.NewServer(Handler(drama, chatServer, agents))
	t.Cleanup(server.Close)
	return server
}

func TestHealthz(t *testing.T) {
	server := newAppServer(t)
	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("healthz = %d %q", resp.StatusCode, body)
	}
}

// TestWebUI 从 "/" 提供 Agent 集市外壳，目录数据来自 /v1/agents；
// 漫剧工坊位于二级页面。两者都不遮挡 API。
func TestWebUI(t *testing.T) {
	server := newAppServer(t)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get marketplace: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Agent 集市")) {
		t.Fatalf("marketplace = %d", resp.StatusCode)
	}

	// 目录接口应含全部 9 个入口：漫剧工坊在首位，其余是 chat 页。
	var catalog struct {
		Agents []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
			Href string `json:"href"`
		} `json:"agents"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/agents", nil, http.StatusOK, &catalog)
	wantHrefs := map[string]string{
		"comic-drama":         "/agents/comic-drama/",
		"assistant":           "/agents/assistant/chat/",
		"creative-studio":     "/agents/creative-studio/chat/",
		"media-creator":       "/agents/media-creator/chat/",
		"web-research":        "/agents/web-research/chat/",
		"document-research":   "/agents/document-research/chat/",
		"video-understanding": "/agents/video-understanding/chat/",
		"audio":               "/agents/audio/chat/",
		"video-editor":        "/agents/video-editor/chat/",
	}
	if len(catalog.Agents) != len(wantHrefs) {
		t.Fatalf("agents = %d, want %d", len(catalog.Agents), len(wantHrefs))
	}
	if catalog.Agents[0].ID != "comic-drama" || catalog.Agents[0].Kind != "workshop" {
		t.Fatalf("first agent = %+v", catalog.Agents[0])
	}
	for _, agent := range catalog.Agents {
		if agent.Href != wantHrefs[agent.ID] {
			t.Fatalf("agent %s href = %q, want %q", agent.ID, agent.Href, wantHrefs[agent.ID])
		}
	}

	// 资产库页面。
	resp, err = http.Get(server.URL + "/assets/")
	if err != nil {
		t.Fatalf("get assets page: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("资产库")) {
		t.Fatalf("assets page = %d", resp.StatusCode)
	}

	resp, err = http.Get(server.URL + "/agents/comic-drama/")
	if err != nil {
		t.Fatalf("get comic-drama workshop: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("workshop status = %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("漫剧工坊")) ||
		!bytes.Contains(body, []byte("返回 Agent 集市")) {
		t.Fatalf("workshop page does not contain expected navigation")
	}
	// 九个预设风格应完整出现在页面里。
	for _, label := range []string{
		"赛博朋克霓虹风", "定格动画黏土风", "日漫赛璐珞风",
		"古风3D国漫", "皮克斯式3D动画", "新中式水墨风",
		"写实电影风", "欧美漫画风", "治愈绘本风",
	} {
		if !bytes.Contains(body, []byte(label)) {
			t.Fatalf("index page missing style preset %q", label)
		}
	}
}

// 资产库：漫剧产物出现在 /v1/assets 项目清单里；下载代理只放行
// 已登记的 URI 并强制 attachment。
func TestAssetLibrary(t *testing.T) {
	assetHost := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		}))
	t.Cleanup(assetHost.Close)
	imageURL := assetHost.URL + "/char1.png"

	fake := coretest.NewFakeAgentK()
	agents := chat.DefaultRegistry()
	drama := comicdrama.NewServer(
		core.ModeAgent,
		fake,
		comicdrama.NewMemoryDramaStore(),
		json.RawMessage(`{"system":"漫剧"}`),
		core.ModelRefs{},
	)
	chatServer := chat.NewServer(fake, agents, nil)
	server := httptest.NewServer(Handler(drama, chatServer, agents))
	t.Cleanup(server.Close)

	// 没有任何项目时返回空数组而不是 null。
	var empty struct {
		Projects []json.RawMessage `json:"projects"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/assets", nil, http.StatusOK, &empty)
	if empty.Projects == nil || len(empty.Projects) != 0 {
		t.Fatalf("empty projects = %v", empty.Projects)
	}

	var created struct {
		ID string `json:"id"`
	}
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]string{"brief": "一只猫的漫剧", "title": "资产库测试"},
		http.StatusAccepted, &created)

	fake.Seed(t, "ses_0001", "round.started", map[string]string{})
	fake.Seed(t, "ses_0001", "assistant.tool_call", map[string]any{
		"toolCallId": "call_img", "toolName": "generate_image",
	})
	fake.Seed(t, "ses_0001", "round.completed", map[string]any{})
	fake.Seed(t, "ses_0001", "tool.exec_succeeded", map[string]any{
		"toolCallId": "call_img",
		"artifacts":  []map[string]string{{"type": "image", "uri": imageURL}},
	})
	// 详情刷新把产物折叠进内存现场，资产库读的是这份缓存。
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID,
		nil, http.StatusOK, nil)

	var library struct {
		Projects []core.AssetProject `json:"projects"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/assets", nil, http.StatusOK, &library)
	if len(library.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(library.Projects))
	}
	project := library.Projects[0]
	if project.Kind != "drama" || project.Title != "资产库测试" || project.Agent != "漫剧工坊" {
		t.Fatalf("project = %+v", project)
	}
	if len(project.Assets) != 1 || project.Assets[0].URI != imageURL ||
		project.Assets[0].Kind != "image" || project.Assets[0].Tool != "generate_image" {
		t.Fatalf("assets = %+v", project.Assets)
	}

	// 下载代理：已登记的 URI 放行并带 attachment；未登记的 404。
	resp, err := http.Get(server.URL + "/v1/assets/download?uri=" + url.QueryEscape(imageURL))
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	payload, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(payload) != "png-bytes" {
		t.Fatalf("download = %d %q", resp.StatusCode, payload)
	}
	if got := resp.Header.Get("Content-Disposition"); got != `attachment; filename="char1.png"` {
		t.Fatalf("content-disposition = %q", got)
	}

	resp, err = http.Get(server.URL + "/v1/assets/download?uri=" +
		url.QueryEscape(assetHost.URL+"/not-registered.png"))
	if err != nil {
		t.Fatalf("download unknown: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown uri status = %d, want 404", resp.StatusCode)
	}
}

func TestChatWebRoutes(t *testing.T) {
	server := newAppServer(t)
	for _, path := range []string{
		"/agents/creative-studio/chat/",
		"/chat.css",
		"/chat.js",
		"/art-modern.css",
	} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get %s status = %d, body: %s", path, response.StatusCode, raw)
		}
	}
	// workshop 形态没有聊天页。
	response, err := http.Get(server.URL + "/agents/comic-drama/chat/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("comic chat status = %d, want 404", response.StatusCode)
	}

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err = client.Get(server.URL + "/agents/basic-assistant/chat/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/" {
		t.Fatalf("removed basic chat redirect = %d %q",
			response.StatusCode, response.Header.Get("Location"))
	}
}
