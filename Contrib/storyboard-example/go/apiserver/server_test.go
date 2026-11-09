package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"google.golang.org/protobuf/proto"
)

type fakeAgentKClient struct {
	createSession func(context.Context, string, *apiv1.Profile) (string, error)
	startPipeline func(context.Context, string, StoryboardArgs) (string, error)
	getRun        func(context.Context, string, string) (PipelineSnapshot, error)
}

func (f *fakeAgentKClient) CreateSession(
	ctx context.Context,
	title string,
	profile *apiv1.Profile,
) (string, error) {
	return f.createSession(ctx, title, profile)
}

func (f *fakeAgentKClient) StartPipeline(
	ctx context.Context,
	sessionID string,
	args StoryboardArgs,
) (string, error) {
	return f.startPipeline(ctx, sessionID, args)
}

func (f *fakeAgentKClient) GetPipelineRun(
	ctx context.Context,
	sessionID string,
	execID string,
) (PipelineSnapshot, error) {
	return f.getRun(ctx, sessionID, execID)
}

func TestCreateAndGetJob(t *testing.T) {
	profile := &apiv1.Profile{
		DefaultModel: "deepseek-v4-pro",
		Pipelines: []*apiv1.ProfilePipeline{{
			Capability: "contrib.storyboard_example.storyboard_film_go",
			Name:       profilePipelineName,
		}},
	}
	var receivedArgs StoryboardArgs
	agentk := &fakeAgentKClient{
		createSession: func(
			_ context.Context,
			title string,
			receivedProfile *apiv1.Profile,
		) (string, error) {
			if title != "Storyboard Film: rain city" {
				t.Fatalf("title = %q", title)
			}
			if !proto.Equal(receivedProfile, profile) {
				t.Fatalf("profile = %s", receivedProfile)
			}
			return "ses_test", nil
		},
		startPipeline: func(
			_ context.Context,
			sessionID string,
			args StoryboardArgs,
		) (string, error) {
			if sessionID != "ses_test" {
				t.Fatalf("sessionID = %q", sessionID)
			}
			receivedArgs = args
			return "exe_test", nil
		},
		getRun: func(
			_ context.Context,
			sessionID string,
			execID string,
		) (PipelineSnapshot, error) {
			if sessionID != "ses_test" || execID != "exe_test" {
				t.Fatalf("coordinates = %s/%s", sessionID, execID)
			}
			return PipelineSnapshot{
				CapabilityID: "contrib.storyboard_example.storyboard_film_go",
				Status:       string(JobCompleted),
				Output: &StoryboardOutput{
					Artifacts: []Artifact{{
						Type:        "video",
						URI:         "https://video.example.com/film.mp4",
						Description: "rain city film, 1 clip",
						Model:       "kling-3.0",
					}},
					ClipCount: 1,
				},
			}, nil
		},
	}
	server := NewServer(agentk, NewMemoryJobStore(), profile, Models{
		Chat:  "deepseek-v4-pro",
		Image: "z-image-turbo",
		Video: "kling-3.0",
	})
	server.newID = func() (string, error) { return "job_test", nil }
	now := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)
	server.now = func() time.Time {
		current := now
		now = now.Add(time.Minute)
		return current
	}

	created := requestJSON(
		t,
		server.Handler(),
		http.MethodPost,
		"/v1/jobs",
		`{"topic":" rain city "}`,
	)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	var createBody map[string]any
	decodeResponse(t, created, &createBody)
	if createBody["id"] != "job_test" || createBody["status"] != "running" {
		t.Fatalf("create body = %#v", createBody)
	}
	if _, ok := createBody["sessionId"]; ok {
		t.Fatalf("sessionId leaked: %#v", createBody)
	}
	if _, ok := createBody["executionId"]; ok {
		t.Fatalf("executionId leaked: %#v", createBody)
	}
	if receivedArgs != (StoryboardArgs{
		Topic:      "rain city",
		Shots:      1,
		ChatModel:  "deepseek-v4-pro",
		ImageModel: "z-image-turbo",
		VideoModel: "kling-3.0",
	}) {
		t.Fatalf("args = %#v", receivedArgs)
	}

	got := requestJSON(
		t,
		server.Handler(),
		http.MethodGet,
		"/v1/jobs/job_test",
		"",
	)
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", got.Code, got.Body)
	}
	var getBody jobDTO
	decodeResponse(t, got, &getBody)
	if getBody.Status != JobCompleted || getBody.Output == nil {
		t.Fatalf("get body = %#v", getBody)
	}
	if len(getBody.Output.Artifacts) != 1 ||
		getBody.Output.Artifacts[0].URI != "https://video.example.com/film.mp4" ||
		getBody.Output.Artifacts[0].Type != "video" ||
		getBody.Output.ClipCount != 1 ||
		getBody.FinishedAt == nil {
		t.Fatalf("get body = %#v", getBody)
	}
}

func TestCreateJobValidation(t *testing.T) {
	calls := 0
	agentk := &fakeAgentKClient{
		createSession: func(
			context.Context,
			string,
			*apiv1.Profile,
		) (string, error) {
			calls++
			return "ses_unexpected", nil
		},
		startPipeline: func(
			context.Context,
			string,
			StoryboardArgs,
		) (string, error) {
			t.Fatal("StartPipeline must not be called")
			return "", nil
		},
		getRun: func(
			context.Context,
			string,
			string,
		) (PipelineSnapshot, error) {
			t.Fatal("GetPipelineRun must not be called")
			return PipelineSnapshot{}, nil
		},
	}
	handler := NewServer(
		agentk,
		NewMemoryJobStore(),
		&apiv1.Profile{},
		Models{},
	).Handler()

	tests := map[string]string{
		"empty topic":     `{"topic":" "}`,
		"shots below":     `{"topic":"x","shots":0}`,
		"shots above":     `{"topic":"x","shots":9}`,
		"unknown field":   `{"topic":"x","other":true}`,
		"malformed":       `{"topic":`,
		"multiple values": `{"topic":"x"} {"topic":"y"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			response := requestJSON(
				t,
				handler,
				http.MethodPost,
				"/v1/jobs",
				body,
			)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("CreateSession calls = %d", calls)
	}
}

func TestStoryboardOutputValidation(t *testing.T) {
	valid := &StoryboardOutput{
		Artifacts: []Artifact{{
			Type:   "video",
			URI:    "https://video.example.com/film.mp4",
			Params: map[string]any{"duration": "5s"},
		}},
		ClipCount: 1,
	}
	if !validStoryboardOutput(valid) {
		t.Fatal("valid output was rejected")
	}

	tests := map[string]*StoryboardOutput{
		"missing output": nil,
		"missing artifacts": {
			ClipCount: 1,
		},
		"missing artifact URI": {
			Artifacts: []Artifact{{Type: "video"}},
			ClipCount: 1,
		},
		"wrong artifact type": {
			Artifacts: []Artifact{{Type: "image", URI: "https://image.example.com/frame.png"}},
			ClipCount: 1,
		},
		"multiple final artifacts": {
			Artifacts: []Artifact{
				{Type: "video", URI: "https://video.example.com/a.mp4"},
				{Type: "video", URI: "https://video.example.com/b.mp4"},
			},
			ClipCount: 2,
		},
		"missing clip count": {
			Artifacts: []Artifact{{Type: "video", URI: "https://video.example.com/film.mp4"}},
		},
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			if validStoryboardOutput(output) {
				t.Fatalf("invalid output accepted: %#v", output)
			}
		})
	}
}

func TestGetFailedJobAndUnknownJob(t *testing.T) {
	store := NewMemoryJobStore()
	createdAt := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)
	if err := store.Create(Job{
		ID:        "job_failed",
		SessionID: "ses_failed",
		ExecID:    "exe_failed",
		Topic:     "rain",
		Shots:     2,
		Status:    JobRunning,
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	agentk := &fakeAgentKClient{
		createSession: func(
			context.Context,
			string,
			*apiv1.Profile,
		) (string, error) {
			t.Fatal("CreateSession must not be called")
			return "", nil
		},
		startPipeline: func(
			context.Context,
			string,
			StoryboardArgs,
		) (string, error) {
			t.Fatal("StartPipeline must not be called")
			return "", nil
		},
		getRun: func(
			context.Context,
			string,
			string,
		) (PipelineSnapshot, error) {
			return PipelineSnapshot{
				CapabilityID: "contrib.storyboard_example.storyboard_film_go",
				Status:       string(JobFailed),
				Error:        "image generation rejected",
			}, nil
		},
	}
	server := NewServer(agentk, store, &apiv1.Profile{}, Models{})
	server.now = func() time.Time { return createdAt.Add(time.Minute) }
	handler := server.Handler()

	failed := requestJSON(
		t,
		handler,
		http.MethodGet,
		"/v1/jobs/job_failed",
		"",
	)
	if failed.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", failed.Code, failed.Body)
	}
	var body jobDTO
	decodeResponse(t, failed, &body)
	if body.Status != JobFailed || body.Error != "image generation rejected" {
		t.Fatalf("body = %#v", body)
	}

	missing := requestJSON(
		t,
		handler,
		http.MethodGet,
		"/v1/jobs/job_missing",
		"",
	)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", missing.Code, missing.Body)
	}
}

func TestListJobsRefreshesRunningJobsInReverseCreationOrder(t *testing.T) {
	store := NewMemoryJobStore()
	firstTime := time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC)
	for _, job := range []Job{
		{
			ID: "job_first", SessionID: "ses_first", ExecID: "exe_first",
			Topic: "first", Shots: 1, Status: JobRunning, CreatedAt: firstTime,
		},
		{
			ID: "job_second", SessionID: "ses_second", ExecID: "exe_second",
			Topic: "second", Shots: 2, Status: JobRunning,
			CreatedAt: firstTime.Add(time.Minute),
		},
	} {
		if err := store.Create(job); err != nil {
			t.Fatal(err)
		}
	}
	polls := 0
	agentk := &fakeAgentKClient{
		createSession: func(
			context.Context,
			string,
			*apiv1.Profile,
		) (string, error) {
			t.Fatal("CreateSession must not be called")
			return "", nil
		},
		startPipeline: func(
			context.Context,
			string,
			StoryboardArgs,
		) (string, error) {
			t.Fatal("StartPipeline must not be called")
			return "", nil
		},
		getRun: func(
			_ context.Context,
			_ string,
			execID string,
		) (PipelineSnapshot, error) {
			polls++
			return PipelineSnapshot{
				CapabilityID: "contrib.storyboard_example.storyboard_film_go",
				Status:       string(JobCompleted),
				Output: &StoryboardOutput{
					Artifacts: []Artifact{{
						Type: "video",
						URI:  "https://video.example.com/" + execID + ".mp4",
					}},
					ClipCount: 1,
				},
			}, nil
		},
	}
	server := NewServer(agentk, store, &apiv1.Profile{}, Models{})
	server.now = func() time.Time { return firstTime.Add(2 * time.Minute) }
	handler := server.Handler()

	for attempt := 0; attempt < 2; attempt++ {
		response := requestJSON(t, handler, http.MethodGet, "/v1/jobs", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		var body struct {
			Jobs []jobDTO `json:"jobs"`
		}
		decodeResponse(t, response, &body)
		if len(body.Jobs) != 2 ||
			body.Jobs[0].ID != "job_second" ||
			body.Jobs[1].ID != "job_first" {
			t.Fatalf("jobs = %#v", body.Jobs)
		}
		for _, job := range body.Jobs {
			if job.Status != JobCompleted {
				t.Fatalf("job = %#v", job)
			}
		}
	}
	if polls != 2 {
		t.Fatalf("polls = %d; terminal jobs should be cached", polls)
	}
}

func TestAgentKFailureReturnsBadGatewayWithoutDetails(t *testing.T) {
	agentk := &fakeAgentKClient{
		createSession: func(
			context.Context,
			string,
			*apiv1.Profile,
		) (string, error) {
			return "", errors.New("upstream rejected secret-api-key")
		},
		startPipeline: func(
			context.Context,
			string,
			StoryboardArgs,
		) (string, error) {
			t.Fatal("StartPipeline must not be called")
			return "", nil
		},
		getRun: func(
			context.Context,
			string,
			string,
		) (PipelineSnapshot, error) {
			t.Fatal("GetPipelineRun must not be called")
			return PipelineSnapshot{}, nil
		},
	}
	handler := NewServer(
		agentk,
		NewMemoryJobStore(),
		&apiv1.Profile{},
		Models{},
	).Handler()
	response := requestJSON(
		t,
		handler,
		http.MethodPost,
		"/v1/jobs",
		`{"topic":"rain"}`,
	)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "secret-api-key") {
		t.Fatalf("response leaked secret: %s", response.Body)
	}
}

func TestHealthz(t *testing.T) {
	handler := NewServer(
		&fakeAgentKClient{},
		NewMemoryJobStore(),
		&apiv1.Profile{},
		Models{},
	).Handler()
	response := requestJSON(t, handler, http.MethodGet, "/healthz", "")
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
}

func TestAPIDocumentation(t *testing.T) {
	handler := NewServer(
		&fakeAgentKClient{},
		NewMemoryJobStore(),
		&apiv1.Profile{},
		Models{},
	).Handler()

	t.Run("OpenAPI YAML", func(t *testing.T) {
		response := requestJSON(t, handler, http.MethodGet, "/openapi.yaml", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/yaml") {
			t.Fatalf("Content-Type = %q", got)
		}
		body := response.Body.String()
		if !strings.Contains(body, "openapi: 3.1.0") ||
			!strings.Contains(body, "/v1/jobs:") {
			t.Fatalf("unexpected OpenAPI YAML: %s", body)
		}
	})

	t.Run("Scalar UI", func(t *testing.T) {
		response := requestJSON(t, handler, http.MethodGet, "/docs", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Fatalf("Content-Type = %q", got)
		}
		body := response.Body.String()
		if !strings.Contains(body, `data-url="/openapi.yaml"`) ||
			!strings.Contains(body, "@scalar/api-reference") {
			t.Fatalf("unexpected Scalar page: %s", body)
		}
	})
}

func requestJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}
