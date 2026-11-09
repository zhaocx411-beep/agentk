package main

import (
	"context"
	"strings"
	"testing"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeAgentKRPC struct {
	create func(*apiv1.CreateSessionRequest) *apiv1.CreateSessionResponse
	start  func(*apiv1.StartExecutionRequest) *apiv1.StartExecutionResponse
	get    func(*apiv1.GetExecutionRequest) *apiv1.GetExecutionResponse
}

func (f *fakeAgentKRPC) CreateSession(
	_ context.Context,
	request *apiv1.CreateSessionRequest,
) (*apiv1.CreateSessionResponse, error) {
	return f.create(request), nil
}

func (f *fakeAgentKRPC) StartExecution(
	_ context.Context,
	request *apiv1.StartExecutionRequest,
) (*apiv1.StartExecutionResponse, error) {
	return f.start(request), nil
}

func (f *fakeAgentKRPC) GetExecution(
	_ context.Context,
	request *apiv1.GetExecutionRequest,
) (*apiv1.GetExecutionResponse, error) {
	return f.get(request), nil
}

func TestSDKAgentKClientPipelineLifecycle(t *testing.T) {
	profile := &apiv1.Profile{
		DefaultModel: "deepseek-v4-pro",
		Pipelines: []*apiv1.ProfilePipeline{{
			Capability: "contrib.storyboard_example.storyboard_film_go",
			Name:       profilePipelineName,
		}},
	}
	output, err := structpb.NewValue(map[string]any{
		"artifacts": []any{map[string]any{
			"type": "video", "uri": "https://video.example.com/film.mp4",
			"description": "rain film, 1 clip", "model": "video",
			"params": map[string]any{"duration": "5s"},
		}},
		"clipCount": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeAgentKRPC{
		create: func(request *apiv1.CreateSessionRequest) *apiv1.CreateSessionResponse {
			if request.GetTitle() != "Storyboard Film: rain" || request.GetProfile() != profile {
				t.Fatalf("create request = %#v", request)
			}
			return &apiv1.CreateSessionResponse{Session: &apiv1.Session{Id: "ses_test"}}
		},
		start: func(request *apiv1.StartExecutionRequest) *apiv1.StartExecutionResponse {
			pipeline := request.GetPipeline()
			if request.GetSessionId() != "ses_test" ||
				pipeline == nil || pipeline.GetPipeline() != profilePipelineName {
				t.Fatalf("start request = %#v", request)
			}
			args := pipeline.GetArgs().AsMap()
			if args["topic"] != "rain" || args["chatModel"] != "chat" ||
				args["imageModel"] != "image" || args["videoModel"] != "video" {
				t.Fatalf("args = %#v", args)
			}
			return &apiv1.StartExecutionResponse{Execution: &apiv1.Execution{Id: "exe_test"}}
		},
		get: func(request *apiv1.GetExecutionRequest) *apiv1.GetExecutionResponse {
			if request.GetSessionId() != "ses_test" || request.GetExecutionId() != "exe_test" {
				t.Fatalf("get request = %#v", request)
			}
			return pipelineExecution(
				apiv1.ExecutionStatus_EXECUTION_STATUS_COMPLETED,
				output,
				"",
			)
		},
	}
	client := &SDKAgentKClient{client: rpc}

	sessionID, err := client.CreateSession(context.Background(), "Storyboard Film: rain", profile)
	if err != nil {
		t.Fatal(err)
	}
	execID, err := client.StartPipeline(context.Background(), sessionID, StoryboardArgs{
		Topic: "rain", Shots: 1,
		ChatModel: "chat", ImageModel: "image", VideoModel: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.GetPipelineRun(context.Background(), sessionID, execID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != string(JobCompleted) || snapshot.Output == nil ||
		len(snapshot.Output.Artifacts) != 1 ||
		snapshot.Output.Artifacts[0].URI != "https://video.example.com/film.mp4" ||
		snapshot.Output.ClipCount != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestSDKAgentKClientMapsCancelledPipelineToFailed(t *testing.T) {
	client := &SDKAgentKClient{client: &fakeAgentKRPC{
		get: func(*apiv1.GetExecutionRequest) *apiv1.GetExecutionResponse {
			return pipelineExecution(
				apiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED,
				nil,
				"",
			)
		},
	}}
	snapshot, err := client.GetPipelineRun(context.Background(), "ses_test", "exe_test")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != string(JobFailed) || snapshot.Error != "pipeline cancelled" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestSDKAgentKClientRejectsInvalidResponses(t *testing.T) {
	tests := map[string]struct {
		response *apiv1.GetExecutionResponse
		want     string
	}{
		"missing execution": {response: &apiv1.GetExecutionResponse{}, want: "missing execution"},
		"not pipeline": {
			response: &apiv1.GetExecutionResponse{Execution: &apiv1.Execution{
				Id: "exe_test", Kind: &apiv1.Execution_Agent{Agent: &apiv1.AgentExecution{}},
			}},
			want: "not a pipeline",
		},
		"unknown status": {
			response: pipelineExecution(apiv1.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED, nil, ""),
			want:     "unknown execution status",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := &SDKAgentKClient{client: &fakeAgentKRPC{
				get: func(*apiv1.GetExecutionRequest) *apiv1.GetExecutionResponse {
					return test.response
				},
			}}
			_, err := client.GetPipelineRun(context.Background(), "ses_test", "exe_test")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func pipelineExecution(
	status apiv1.ExecutionStatus,
	output *structpb.Value,
	errorMessage string,
) *apiv1.GetExecutionResponse {
	return &apiv1.GetExecutionResponse{Execution: &apiv1.Execution{
		Id: "exe_test", SessionId: "ses_test", Status: status, Error: errorMessage,
		Kind: &apiv1.Execution_Pipeline{Pipeline: &apiv1.PipelineExecution{
			Name:         profilePipelineName,
			CapabilityId: "contrib.storyboard_example.storyboard_film_go",
			Output:       output,
		}},
	}}
}
