package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1/apiv1connect"
)

type watchEventsTestHandler struct {
	apiv1connect.UnimplementedAgentKServiceHandler
	t *testing.T
}

func (h watchEventsTestHandler) WatchEvents(
	_ context.Context,
	request *connect.Request[apiv1.WatchEventsRequest],
	stream *connect.ServerStream[apiv1.WatchEventsResponse],
) error {
	if request.Msg.GetSessionId() != "ses_123" || request.Msg.GetExecutionId() != "exe_123" {
		h.t.Fatalf("watch request = %+v", request.Msg)
	}
	frames := []*apiv1.WatchEventsResponse{
		{Frame: &apiv1.WatchEventsResponse_Delta{Delta: &apiv1.Delta{
			ExecutionId: "exe_123", RoundId: "rnd_1", Generation: "gen_1",
			Index: 3, Block: 1, Kind: apiv1.DeltaBlockKind_DELTA_BLOCK_KIND_TEXT,
			Text: "你好",
		}}},
		{Frame: &apiv1.WatchEventsResponse_CommittedEvent{CommittedEvent: &apiv1.Event{
			EventId: "evt_1", ExecutionId: "exe_123", RoundId: "rnd_1",
			Payload: &apiv1.Event_AssistantText{AssistantText: &apiv1.AssistantText{Text: "你好"}},
		}}},
		{Frame: &apiv1.WatchEventsResponse_StreamEnd{StreamEnd: &apiv1.StreamEnd{
			ExecutionId: "exe_123",
		}}},
	}
	for _, frame := range frames {
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	return nil
}

func TestHTTPAgentKClientCreatesSessionWithProtoProfile(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost ||
			r.URL.Path != apiv1connect.AgentKServiceCreateSessionProcedure {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var body struct {
			Title   string `json:"title"`
			Profile struct {
				DefaultModel string `json:"defaultModel"`
				Tools        []struct {
					Capability string `json:"capability"`
					Name       string `json:"name"`
				} `json:"tools"`
			} `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Title != "漫剧" || body.Profile.DefaultModel != "deepseek-v4-pro" ||
			len(body.Profile.Tools) != 1 ||
			body.Profile.Tools[0].Capability != "agentk.generate_image" {
			t.Fatalf("body = %+v", body)
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"session": map[string]any{"id": "ses_123", "title": "漫剧"},
		})
	}))
	defer upstream.Close()

	client, err := NewHTTPAgentKClient(upstream.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.CreateSession(context.Background(), "漫剧", json.RawMessage(`{
		"defaultModel":"deepseek-v4-pro",
		"tools":[{"capability":"agentk.generate_image","name":"generate_image"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if id != "ses_123" {
		t.Fatalf("session ID = %q", id)
	}
}

func TestHTTPAgentKClientWatchesExecutionEvents(t *testing.T) {
	_, handler := apiv1connect.NewAgentKServiceHandler(watchEventsTestHandler{t: t})
	upstream := httptest.NewServer(handler)
	defer upstream.Close()

	client, err := NewHTTPAgentKClient(upstream.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var frames []AgentKStreamFrame
	err = client.WatchEvents(context.Background(), "ses_123", "exe_123", func(frame AgentKStreamFrame) error {
		frames = append(frames, frame)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 || frames[0].Delta == nil || frames[0].Delta.Text != "你好" ||
		frames[0].Delta.Kind != "text" || frames[1].Event == nil ||
		frames[1].Event.Kind != "assistant.text" || !frames[2].End {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestDecodeAgentKProfileRejectsLegacyRef(t *testing.T) {
	_, err := DecodeAgentKProfile(json.RawMessage(`{
		"defaultModel":"deepseek-v4-pro",
		"tools":[{"ref":"tool:agentk.generate_image","name":"generate_image"}]
	}`))
	if err == nil {
		t.Fatal("legacy ref should be rejected")
	}
}

func TestHTTPAgentKClientAnswersQuestionsWithStartExecution(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost ||
			r.URL.Path != apiv1connect.AgentKServiceStartExecutionProcedure {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			SessionID string `json:"sessionId"`
			Answers   struct {
				ToolCallID string       `json:"toolCallId"`
				Answers    []ChatAnswer `json:"answers"`
			} `json:"answers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.SessionID != "ses_123" || body.Answers.ToolCallID != "call_ask" ||
			len(body.Answers.Answers) != 1 ||
			body.Answers.Answers[0].QuestionID != "q1" ||
			body.Answers.Answers[0].Selected[0] != "水墨" {
			t.Fatalf("body = %+v", body)
		}
		WriteJSON(w, http.StatusOK, agentExecutionResponse("ses_123", "exe_answer"))
	}))
	defer upstream.Close()

	client, err := NewHTTPAgentKClient(upstream.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	err = client.AnswerQuestions(context.Background(), "ses_123", "call_ask",
		[]ChatAnswer{{QuestionID: "q1", Selected: []string{"水墨"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAgentKClientStartsAndReadsPipelineExecution(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case apiv1connect.AgentKServiceStartExecutionProcedure:
			var body struct {
				SessionID string `json:"sessionId"`
				Pipeline  struct {
					Pipeline string         `json:"pipeline"`
					Args     map[string]any `json:"args"`
				} `json:"pipeline"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode start body: %v", err)
			}
			if body.SessionID != "ses_123" || body.Pipeline.Pipeline != pipelineName ||
				body.Pipeline.Args["brief"] != "小鸡吃米" ||
				body.Pipeline.Args["imageModel"] != "gpt-image-2" {
				t.Fatalf("start body = %+v", body)
			}
			WriteJSON(w, http.StatusOK, pipelineExecutionResponse(
				"ses_123", "exe_pipeline", "EXECUTION_STATUS_RUNNING", nil, "",
			))
		case apiv1connect.AgentKServiceGetExecutionProcedure:
			WriteJSON(w, http.StatusOK, pipelineExecutionResponse(
				"ses_123", "exe_pipeline", "EXECUTION_STATUS_COMPLETED",
				map[string]any{
					"title": "小鸡吃米", "script": "剧本", "styleAnchor": "漫画",
					"characters": []any{}, "episodes": []any{},
					"film": "https://example.test/film.mp4", "clipCount": 1,
				}, "",
			))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer upstream.Close()

	client, err := NewHTTPAgentKClient(upstream.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	executionID, err := client.StartPipeline(context.Background(), "ses_123", DramaArgs{
		Brief: "小鸡吃米", ImageModel: "gpt-image-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executionID != "exe_pipeline" {
		t.Fatalf("execution ID = %q", executionID)
	}
	snapshot, err := client.GetPipelineRun(context.Background(), "ses_123", executionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "completed" || snapshot.Output == nil ||
		snapshot.Output.Film != "https://example.test/film.mp4" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestHTTPAgentKClientListsAllEventPagesOldestFirst(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiv1connect.AgentKServiceListEventsProcedure {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		var body struct {
			SessionID string `json:"sessionId"`
			PageToken string `json:"pageToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		requests++
		switch body.PageToken {
		case "":
			WriteJSON(w, http.StatusOK, map[string]any{
				"events": []any{
					map[string]any{
						"eventId": "evt_0003", "executionId": "exe_1",
						"executionFinished": map[string]any{
							"status": "EXECUTION_STATUS_COMPLETED",
						},
					},
					map[string]any{
						"eventId": "evt_0002", "executionId": "exe_1",
						"invocationProgress": map[string]any{
							"invocationId":   "inv_1",
							"invocationKind": "INVOCATION_KIND_PIPELINE",
							"invocationName": pipelineName,
							"stepId":         "storyboard", "message": "分镜完成",
						},
					},
				},
				"nextPageToken": "older",
			})
		case "older":
			WriteJSON(w, http.StatusOK, map[string]any{
				"events": []any{
					map[string]any{
						"eventId": "evt_0001", "executionId": "exe_1",
						"userInput": map[string]any{"text": "做一部漫剧"},
					},
				},
			})
		default:
			t.Fatalf("unexpected page token %q", body.PageToken)
		}
	}))
	defer upstream.Close()

	client, err := NewHTTPAgentKClient(upstream.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.ListEvents(context.Background(), "ses_123")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(events) != 3 {
		t.Fatalf("requests = %d, events = %d", requests, len(events))
	}
	gotIDs := []string{events[0].ID, events[1].ID, events[2].ID}
	if !reflect.DeepEqual(gotIDs, []string{"evt_0001", "evt_0002", "evt_0003"}) {
		t.Fatalf("event order = %v", gotIDs)
	}
	if events[0].Kind != "user.input" || events[1].Kind != "invocation.progress" ||
		events[2].Kind != "execution.finished" {
		t.Fatalf("event kinds = %q, %q, %q",
			events[0].Kind, events[1].Kind, events[2].Kind)
	}
	var progress struct {
		InvocationKind string `json:"invocationKind"`
	}
	if err := json.Unmarshal(events[1].Data, &progress); err != nil ||
		progress.InvocationKind != "pipeline" {
		t.Fatalf("progress = %+v, err = %v", progress, err)
	}
	var finished struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(events[2].Data, &finished); err != nil ||
		finished.Status != "completed" {
		t.Fatalf("finished = %+v, err = %v", finished, err)
	}
}

func agentExecutionResponse(sessionID, executionID string) map[string]any {
	return map[string]any{"execution": map[string]any{
		"id": executionID, "sessionId": sessionID,
		"status": "EXECUTION_STATUS_RUNNING", "agent": map[string]any{},
	}}
}

func pipelineExecutionResponse(
	sessionID, executionID, status string,
	output any,
	errorMessage string,
) map[string]any {
	pipeline := map[string]any{"name": pipelineName}
	if output != nil {
		pipeline["output"] = output
	}
	return map[string]any{"execution": map[string]any{
		"id": executionID, "sessionId": sessionID, "status": status,
		"error": errorMessage, "pipeline": pipeline,
	}}
}
