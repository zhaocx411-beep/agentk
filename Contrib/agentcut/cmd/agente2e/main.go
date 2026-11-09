package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/requestlane"

	"agentcut/tools"
)

const e2eProfileID = "agentcut-e2e"

var safeIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type runnerConfig struct {
	BaseURL       string
	User          string
	Lane          string
	InternalQueue string
	Timeout       time.Duration
}

type apiClient struct {
	config runnerConfig
	client *http.Client
}

type projectRef struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
}

type sessionEvent struct {
	EventID     string         `json:"eventId"`
	ExecutionID string         `json:"executionId"`
	Kind        string         `json:"kind"`
	Data        map[string]any `json:"data"`
}

func main() {
	apiURL := flag.String("api", envOr("AGENTCUT_API_URL", "http://127.0.0.1:8080"), "AgentCut API base URL")
	timeout := flag.Duration("timeout", 3*time.Minute, "whole E2E timeout")
	flag.Parse()

	config, err := loadRunnerConfig(os.Getenv, *apiURL, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	if err := run(ctx, &apiClient{
		config: config,
		client: &http.Client{Timeout: 35 * time.Second},
	}); err != nil {
		log.Fatalf("AgentCut staging E2E failed: %v", err)
	}
}

func loadRunnerConfig(
	getenv func(string) string,
	baseURL string,
	timeout time.Duration,
) (runnerConfig, error) {
	if strings.ToLower(strings.TrimSpace(getenv("AGENTCUT_E2E_ALLOW_STAGING_WRITES"))) != "true" {
		return runnerConfig{}, errors.New(
			"AGENTCUT_E2E_ALLOW_STAGING_WRITES=true is required",
		)
	}
	lane := strings.TrimSpace(getenv("KWS_LANE_ID"))
	if lane == "" {
		return runnerConfig{}, errors.New("a non-empty KWS_LANE_ID is required")
	}
	if _, err := requestlane.Parse(lane); err != nil {
		return runnerConfig{}, fmt.Errorf("a valid non-empty KWS_LANE_ID is required: %w", err)
	}
	user := strings.TrimSpace(getenv("AGENTCUT_DEV_USER"))
	if user == "dev" || !safeIdentity.MatchString(user) {
		return runnerConfig{}, errors.New(
			"AGENTCUT_DEV_USER must be a non-default personal user",
		)
	}
	queue := strings.TrimSpace(getenv("AGENTCUT_TASK_QUEUE"))
	if queue == "" || queue == tools.InternalTaskQueue || queue == tools.WorkerTaskQueue {
		return runnerConfig{}, errors.New(
			"AGENTCUT_TASK_QUEUE must be an explicit private internal queue",
		)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return runnerConfig{}, fmt.Errorf("invalid AgentCut API URL %q", baseURL)
	}
	if timeout <= 0 {
		return runnerConfig{}, errors.New("E2E timeout must be positive")
	}
	return runnerConfig{
		BaseURL: baseURL, User: user, Lane: lane, InternalQueue: queue, Timeout: timeout,
	}, nil
}

func run(ctx context.Context, api *apiClient) error {
	if err := api.health(ctx); err != nil {
		return fmt.Errorf("health preflight: %w", err)
	}
	if err := api.requireE2EProfile(ctx); err != nil {
		return fmt.Errorf("profile preflight: %w", err)
	}

	title := fmt.Sprintf(
		"agentcut-e2e/%s/%s",
		api.config.User,
		time.Now().UTC().Format("20060102T150405.000000000Z"),
	)
	var project projectRef
	if err := api.jsonRequest(ctx, http.MethodPost, "/v1/projects", map[string]any{
		"title": title, "profileId": e2eProfileID,
	}, &project); err != nil {
		return fmt.Errorf("create project and Agent-K session: %w", err)
	}
	if project.ProjectID == "" || project.SessionID == "" {
		return fmt.Errorf("create project returned empty projectId or sessionId")
	}
	log.Printf(
		"AgentCut E2E created: projectId=%s sessionId=%s user=%q lane=%q",
		project.ProjectID, project.SessionID, api.config.User, api.config.Lane,
	)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		err := api.jsonRequest(
			cleanupCtx,
			http.MethodDelete,
			"/v1/projects/"+url.PathEscape(project.ProjectID),
			nil,
			nil,
		)
		if err != nil {
			log.Printf(
				"ERROR: E2E project cleanup failed: projectId=%s sessionId=%s error=%v",
				project.ProjectID, project.SessionID, err,
			)
		} else {
			log.Printf(
				"AgentCut E2E project deleted; Agent-K has no DeleteSession, audit sessionId=%s",
				project.SessionID,
			)
		}
	}()

	var started struct {
		ExecutionID string `json:"executionId"`
	}
	messagePath := "/v1/projects/" + url.PathEscape(project.ProjectID) + "/messages"
	if err := api.jsonRequest(ctx, http.MethodPost, messagePath, map[string]any{
		"profileId": e2eProfileID,
		"text":      "只调用一次 timeline_get，返回当前 revision 和轨道数；不要修改，不要生成或处理素材。",
	}, &started); err != nil {
		return fmt.Errorf("start minimal Agent-K execution: %w", err)
	}
	if started.ExecutionID == "" {
		return errors.New("start execution returned an empty executionId")
	}

	frameCount, err := api.consumeSSE(ctx, project.ProjectID, started.ExecutionID)
	if err != nil {
		return fmt.Errorf("consume execution SSE: %w", err)
	}
	events, err := api.waitForEvents(ctx, project.ProjectID, started.ExecutionID)
	if err != nil {
		return fmt.Errorf("poll execution events: %w", err)
	}
	if err := validateEvents(events, started.ExecutionID); err != nil {
		return fmt.Errorf("validate timeline_get-only execution: %w", err)
	}
	log.Printf(
		"AgentCut staging E2E passed: executionId=%s sseFrames=%d events=%d",
		started.ExecutionID, frameCount, len(events),
	)
	return nil
}

func (a *apiClient) health(ctx context.Context) error {
	return a.jsonRequest(ctx, http.MethodGet, "/healthz", nil, nil)
}

func (a *apiClient) requireE2EProfile(ctx context.Context) error {
	var response struct {
		Profiles []struct {
			ProfileID string `json:"profileId"`
		} `json:"profiles"`
	}
	if err := a.jsonRequest(ctx, http.MethodGet, "/v1/profiles", nil, &response); err != nil {
		return err
	}
	for _, profile := range response.Profiles {
		if profile.ProfileID == e2eProfileID {
			return nil
		}
	}
	return errors.New(
		"agentcut-e2e is not registered; restart API with AGENTCUT_E2E_PROFILE=true",
	)
}

func (a *apiClient) jsonRequest(
	ctx context.Context,
	method string,
	path string,
	body any,
	output any,
) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, a.config.BaseURL+path, reader)
	if err != nil {
		return err
	}
	a.setHeaders(request)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf(
			"%s %s: HTTP %d: %s",
			method, path, response.StatusCode, strings.TrimSpace(string(raw)),
		)
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (a *apiClient) setHeaders(request *http.Request) {
	request.Header.Set("X-AgentCut-User", a.config.User)
	raw, _ := json.Marshal(map[string]string{"laneId": a.config.Lane})
	request.Header.Set("trace-context", string(raw))
}

func (a *apiClient) consumeSSE(
	ctx context.Context,
	projectID string,
	executionID string,
) (int, error) {
	path := "/v1/projects/" + url.PathEscape(projectID) +
		"/stream?executionId=" + url.QueryEscape(executionID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.config.BaseURL+path, nil)
	if err != nil {
		return 0, err
	}
	a.setHeaders(request)
	response, err := a.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return 0, fmt.Errorf(
			"GET %s: HTTP %d: %s",
			path, response.StatusCode, strings.TrimSpace(string(raw)),
		)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return 0, fmt.Errorf(
			"unexpected SSE content type %q",
			response.Header.Get("Content-Type"),
		)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	frames := 0
	streamEnded := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			frames++
		}
		if line == "event: stream.end" {
			streamEnded = true
		}
	}
	if err := scanner.Err(); err != nil {
		return frames, err
	}
	if frames == 0 || !streamEnded {
		return frames, fmt.Errorf(
			"SSE ended without committed frames or stream.end (frames=%d, streamEnd=%t)",
			frames, streamEnded,
		)
	}
	return frames, nil
}

func (a *apiClient) waitForEvents(
	ctx context.Context,
	projectID string,
	executionID string,
) ([]sessionEvent, error) {
	path := "/v1/projects/" + url.PathEscape(projectID) + "/events?limit=50"
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var page struct {
			Events []sessionEvent `json:"events"`
		}
		if err := a.jsonRequest(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		for _, event := range page.Events {
			if event.ExecutionID == executionID && event.Kind == "execution.finished" {
				return page.Events, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("execution did not reach a terminal event: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func validateEvents(events []sessionEvent, executionID string) error {
	var toolCalls []sessionEvent
	succeededCalls := make(map[string]bool)
	terminalStatus := ""
	terminalError := ""
	for _, event := range events {
		if event.ExecutionID != executionID {
			continue
		}
		switch event.Kind {
		case "assistant.tool_call":
			toolCalls = append(toolCalls, event)
		case "tool.exec_succeeded":
			succeededCalls[stringValue(event.Data["toolCallId"])] = true
		case "tool.exec_failed":
			return fmt.Errorf("tool execution failed: %v", event.Data)
		case "execution.finished":
			terminalStatus = stringValue(event.Data["status"])
			terminalError = stringValue(event.Data["error"])
		}
	}
	if len(toolCalls) != 1 {
		return fmt.Errorf("expected exactly one tool call, got %d", len(toolCalls))
	}
	toolName := stringValue(toolCalls[0].Data["toolName"])
	if toolName != "timeline_get" {
		return fmt.Errorf("unexpected tool call %q; only timeline_get is allowed", toolName)
	}
	toolCallID := stringValue(toolCalls[0].Data["toolCallId"])
	if toolCallID == "" || !succeededCalls[toolCallID] {
		return fmt.Errorf("timeline_get tool call %q has no matching success event", toolCallID)
	}
	normalizedStatus := strings.ToUpper(terminalStatus)
	if !strings.Contains(normalizedStatus, "SUCCEEDED") &&
		!strings.Contains(normalizedStatus, "COMPLETED") {
		return fmt.Errorf(
			"execution finished with status %q: %s",
			terminalStatus, terminalError,
		)
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
