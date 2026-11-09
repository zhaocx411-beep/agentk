package comicdrama

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"studio/platform/core"
	"studio/platform/core/coretest"
)

func testProfile() json.RawMessage {
	return json.RawMessage(`{"system":"漫剧 pipeline 宿主会话","pipelines":[{"name":"comic_drama_film"}]}`)
}

func testModels() core.ModelRefs {
	return core.ModelRefs{Chat: "chat-cap", Image: "image-cap", Edit: "edit-cap", Video: "video-cap"}
}

func newTestServer(t *testing.T) (*httptest.Server, *coretest.FakeAgentK) {
	t.Helper()
	fake := coretest.NewFakeAgentK()
	server := httptest.NewServer(
		NewServer(core.ModePipeline, fake, NewMemoryDramaStore(), testProfile(), testModels()).Handler(),
	)
	t.Cleanup(server.Close)
	return server, fake
}

func newAgentTestServer(t *testing.T) (*httptest.Server, *coretest.FakeAgentK) {
	t.Helper()
	fake := coretest.NewFakeAgentK()
	server := httptest.NewServer(
		NewServer(core.ModeAgent, fake, NewMemoryDramaStore(), testProfile(), testModels()).Handler(),
	)
	t.Cleanup(server.Close)
	return server, fake
}

func TestCreateDrama(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{
			"brief":       "深夜食堂的猫店长",
			"stylePrompt": "治愈绘本风格",
		}, http.StatusAccepted, &created)

	if created.ID == "" || created.Status != DramaRunning {
		t.Fatalf("created = %+v", created)
	}
	if created.Title != "深夜食堂的猫店长" {
		t.Fatalf("title = %q", created.Title)
	}

	// fake 里应有一个会话、一次已启动的 pipeline run；模型引用由服务端填写。
	fake.Mu.Lock()
	defer fake.Mu.Unlock()
	if len(fake.Sessions) != 1 || len(fake.Runs) != 1 {
		t.Fatalf("sessions = %d, runs = %d", len(fake.Sessions), len(fake.Runs))
	}
	for _, args := range fake.StartArgs {
		if args.Brief != "深夜食堂的猫店长" || args.StylePrompt != "治愈绘本风格" {
			t.Fatalf("args = %+v", args)
		}
		if args.ChatModel != "chat-cap" || args.ImageModel != "image-cap" ||
			args.EditModel != "edit-cap" || args.VideoModel != "video-cap" {
			t.Fatalf("model refs not filled: %+v", args)
		}
	}
	for id := range fake.Sessions {
		if !bytes.Contains(fake.Profiles[id], []byte("comic_drama_film")) {
			t.Fatalf("profile not forwarded: %s", fake.Profiles[id])
		}
	}
}

func TestCreateDramaValidation(t *testing.T) {
	server, _ := newTestServer(t)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "  "}, http.StatusBadRequest, nil)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"unknown": 1}, http.StatusBadRequest, nil)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "猫", "maxShots": 99}, http.StatusBadRequest, nil)
}

func TestCreateDramaAgentKDown(t *testing.T) {
	server, fake := newTestServer(t)
	fake.FailCreate = true
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusBadGateway, nil)

	fake.FailCreate = false
	fake.FailStart = true
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusBadGateway, nil)
}

func TestDramaLifecycle(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长", "title": "猫店长"}, http.StatusAccepted, &created)

	var sessionID, execID string
	fake.Mu.Lock()
	for id := range fake.Sessions {
		sessionID = id
	}
	for id := range fake.Runs {
		execID = id
	}
	fake.Mu.Unlock()

	// 推进剧情：worker 汇报了两个阶段。
	fake.SeedProgress(t, sessionID, execID, "script", "剧本完成：猫店长")
	fake.SeedProgress(t, sessionID, execID, "characters", "人物设定完成，共 2 人")

	var mid dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &mid)
	if mid.Status != DramaRunning {
		t.Fatalf("mid status = %q, want running", mid.Status)
	}
	if len(mid.Progress) != 2 || mid.Progress[0].StepID != "script" ||
		mid.Progress[1].StepID != "characters" {
		t.Fatalf("progress = %+v", mid.Progress)
	}

	// 完成：快照给出终态与完整资产清单。
	fake.SeedProgress(t, sessionID, execID, "film", "成片完成，共 2 个镜头")
	fake.SetRun(execID, core.PipelineSnapshot{
		Status: string(DramaCompleted),
		Output: &core.DramaManifest{
			Title:  "猫店长",
			Script: "第1集\nS01 深夜食堂 ...",
			Characters: []core.CharacterAsset{
				{Name: "猫店长", Prompt: "橘猫", ImageURL: "https://image/cat.png"},
			},
			Episodes: []core.EpisodeAsset{{
				Number: 1,
				Script: "第1集\nS01 深夜食堂 ...",
				Shots: []core.ShotAsset{
					{Index: 0, ShotID: "01", Scenes: []string{"食堂"}, FrameURL: "https://image/f0.png", VideoURL: "https://video/c0.mp4"},
					{Index: 1, ShotID: "02", Scenes: []string{"食堂"}, FrameURL: "https://image/f1.png", VideoURL: "https://video/c1.mp4"},
				},
				Film:      "https://video/film.mp4",
				ClipCount: 2,
			}},
			Film:      "https://video/film.mp4",
			ClipCount: 2,
		},
	})

	var done dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &done)
	if done.Status != DramaCompleted {
		t.Fatalf("done status = %q, want completed", done.Status)
	}
	if done.Output == nil || done.Output.Film != "https://video/film.mp4" {
		t.Fatalf("output = %+v", done.Output)
	}
	if len(done.Output.Characters) != 1 || len(done.Output.Episodes) != 1 ||
		len(done.Output.Episodes[0].Shots) != 2 {
		t.Fatalf("manifest = %+v", done.Output)
	}
	if len(done.Progress) != 3 || done.Progress[2].StepID != "film" {
		t.Fatalf("progress = %+v", done.Progress)
	}

	// 终态后不再回源：agent-k 挂了也能读。
	fake.FailRun = true
	fake.FailEvents = true
	var cached dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &cached)
	if cached.Status != DramaCompleted || cached.Output == nil {
		t.Fatalf("cached = %+v", cached)
	}

	// 列表也能看到。
	fake.FailRun = false
	fake.FailEvents = false
	var list struct {
		Dramas []dramaDTO `json:"dramas"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas", nil, http.StatusOK, &list)
	if len(list.Dramas) != 1 || list.Dramas[0].ID != created.ID {
		t.Fatalf("list = %+v", list.Dramas)
	}
}

func TestDramaFailure(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	var execID string
	fake.Mu.Lock()
	for id := range fake.Runs {
		execID = id
	}
	fake.Mu.Unlock()
	fake.SetRun(execID, core.PipelineSnapshot{
		Status: string(DramaFailed),
		Error:  "comic-drama: all shots failed",
	})

	var failed dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &failed)
	if failed.Status != DramaFailed || failed.Error != "comic-drama: all shots failed" {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestDramaCancelledIsTerminal(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	var execID string
	fake.Mu.Lock()
	for id := range fake.Runs {
		execID = id
	}
	fake.Mu.Unlock()
	fake.SetRun(execID, core.PipelineSnapshot{
		Status: string(DramaCancelled),
		Error:  "canceled",
	})

	var cancelled dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil,
		http.StatusOK, &cancelled)
	if cancelled.Status != DramaCancelled || cancelled.Error != "canceled" {
		t.Fatalf("cancelled = %+v", cancelled)
	}

	// cancelled 与其他终态一样使用缓存，不再触发远端查询。
	fake.FailRun = true
	fake.FailEvents = true
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil,
		http.StatusOK, &cancelled)
}

func TestGetDramaNotFound(t *testing.T) {
	server, _ := newTestServer(t)
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/drm_missing", nil, http.StatusNotFound, nil)
}

func TestEventsProxy(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	var sessionID, execID string
	fake.Mu.Lock()
	for id := range fake.Sessions {
		sessionID = id
	}
	for id := range fake.Runs {
		execID = id
	}
	fake.Mu.Unlock()
	fake.SeedProgress(t, sessionID, execID, "script", "剧本完成")
	fake.SeedProgress(t, sessionID, execID, "board", "分镜完成")

	var page struct {
		Events []core.LedgerEvent `json:"events"`
	}
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID+"/events", nil,
		http.StatusOK, &page)
	if len(page.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(page.Events))
	}
	if page.Events[0].ID == "" || page.Events[1].ID <= page.Events[0].ID {
		t.Fatalf("event ids must be present and ascending: %+v", page.Events)
	}
}

func TestGetDramaAgentKDown(t *testing.T) {
	server, fake := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	fake.FailRun = true
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil,
		http.StatusBadGateway, nil)
}

/* ================= agent 模式 ================= */

func TestAgentModeCreateDrama(t *testing.T) {
	server, fake := newAgentTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{
			"brief":       "深夜食堂的猫店长",
			"stylePrompt": "治愈系手绘绘本风格",
		}, http.StatusAccepted, &created)

	if created.Mode != core.ModeAgent || created.Status != DramaRunning {
		t.Fatalf("created = %+v", created)
	}

	// fake 里应有一个会话、一条 user.input（brief + 风格锚点），无 pipeline run。
	fake.Mu.Lock()
	defer fake.Mu.Unlock()
	if len(fake.Sessions) != 1 || len(fake.Runs) != 0 {
		t.Fatalf("sessions = %d, runs = %d", len(fake.Sessions), len(fake.Runs))
	}
	for _, events := range fake.Sessions {
		if len(events) != 1 || events[0].Kind != "user.input" {
			t.Fatalf("session events = %+v", events)
		}
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(events[0].Data, &payload); err != nil {
			t.Fatalf("decode user.input: %v", err)
		}
		if !strings.Contains(payload.Text, "深夜食堂的猫店长") ||
			!strings.Contains(payload.Text, "治愈系手绘绘本风格") {
			t.Fatalf("first message = %q", payload.Text)
		}
	}
}

func TestAgentModeLifecycle(t *testing.T) {
	server, fake := newAgentTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长", "title": "猫店长"}, http.StatusAccepted, &created)

	var sessionID string
	fake.Mu.Lock()
	for id := range fake.Sessions {
		sessionID = id
	}
	fake.Mu.Unlock()

	// 推进剧情：一轮工具调用（成片拼接）+ 一轮纯正文收敛。
	fake.Seed(t, sessionID, "round.started", map[string]string{"model": "chat-cap"})
	fake.Seed(t, sessionID, "assistant.tool_call", map[string]any{
		"toolCallId": "call_1", "toolName": "concat_videos",
	})
	fake.Seed(t, sessionID, "round.completed", map[string]any{})

	var mid dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &mid)
	if mid.Status != DramaRunning {
		t.Fatalf("mid status = %q, want running", mid.Status)
	}

	fake.Seed(t, sessionID, "tool.exec_succeeded", map[string]any{
		"toolCallId": "call_1",
		"result":     map[string]any{"videoUrl": "https://files/ep1.mp4", "clipCount": 2},
		"artifacts":  []map[string]string{{"type": "video", "uri": "https://files/ep1.mp4"}},
	})
	fake.Seed(t, sessionID, "round.started", map[string]string{"model": "chat-cap"})
	fake.Seed(t, sessionID, "assistant.text", map[string]any{
		"block": 0, "text": "第 1 集完成：https://files/ep1.mp4",
	})
	fake.Seed(t, sessionID, "round.completed", map[string]any{})

	var done dramaDTO
	coretest.DoJSON(t, http.MethodGet, server.URL+"/v1/dramas/"+created.ID, nil, http.StatusOK, &done)
	if done.Status != DramaIdle {
		t.Fatalf("done status = %q, want idle", done.Status)
	}
	if len(done.Films) != 1 || done.Films[0] != "https://files/ep1.mp4" {
		t.Fatalf("films = %v", done.Films)
	}
	if done.AssistantSays != "第 1 集完成：https://files/ep1.mp4" {
		t.Fatalf("assistantSays = %q", done.AssistantSays)
	}
	if done.VideoCount != 1 {
		t.Fatalf("videoCount = %d", done.VideoCount)
	}

	// 追加消息（下一集）→ 回到 running。
	var next dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas/"+created.ID+"/messages",
		map[string]any{"text": "继续做第 2 集"}, http.StatusAccepted, &next)
	if next.Status != DramaRunning {
		t.Fatalf("status after follow-up = %q, want running", next.Status)
	}
}

func TestAgentModeMessageValidation(t *testing.T) {
	server, _ := newAgentTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas/"+created.ID+"/messages",
		map[string]any{"text": " "}, http.StatusBadRequest, nil)
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas/drm_missing/messages",
		map[string]any{"text": "hi"}, http.StatusNotFound, nil)
}

// TestPipelineModeHasNoMessagesEndpoint pipeline 是一次性执行，不注册
// /messages 路由；ServeMux 对"路径匹配但方法不匹配"回 405。
func TestPipelineModeHasNoMessagesEndpoint(t *testing.T) {
	server, _ := newTestServer(t)

	var created dramaDTO
	coretest.DoJSON(t, http.MethodPost, server.URL+"/v1/dramas",
		map[string]any{"brief": "深夜食堂的猫店长"}, http.StatusAccepted, &created)

	req, err := http.NewRequest(http.MethodPost,
		server.URL+"/v1/dramas/"+created.ID+"/messages",
		strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404/405", resp.StatusCode)
	}
}
