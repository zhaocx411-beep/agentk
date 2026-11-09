package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentkclient "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client"
	agentkgrpc "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/client/grpc"
	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

const profilePipelineName = "storyboard_film_example"

type StoryboardArgs struct {
	Topic      string `json:"topic"`
	Shots      int    `json:"shots"`
	ChatModel  string `json:"chatModel"`
	ImageModel string `json:"imageModel"`
	VideoModel string `json:"videoModel"`
}

type Artifact struct {
	Type        string         `json:"type"`
	URI         string         `json:"uri"`
	Description string         `json:"description,omitempty"`
	Model       string         `json:"model,omitempty"`
	Params      map[string]any `json:"params,omitempty"`
}

type StoryboardOutput struct {
	Artifacts []Artifact `json:"artifacts"`
	ClipCount int        `json:"clipCount"`
}

type PipelineSnapshot struct {
	CapabilityID string
	Status       string
	Output       *StoryboardOutput
	Error        string
}

type AgentKClient interface {
	CreateSession(context.Context, string, *apiv1.Profile) (string, error)
	StartPipeline(context.Context, string, StoryboardArgs) (string, error)
	GetPipelineRun(context.Context, string, string) (PipelineSnapshot, error)
}

type agentKRPC interface {
	CreateSession(context.Context, *apiv1.CreateSessionRequest) (*apiv1.CreateSessionResponse, error)
	StartExecution(context.Context, *apiv1.StartExecutionRequest) (*apiv1.StartExecutionResponse, error)
	GetExecution(context.Context, *apiv1.GetExecutionRequest) (*apiv1.GetExecutionResponse, error)
}

type SDKAgentKClient struct {
	client agentKRPC
}

func NewAgentKClient() (*SDKAgentKClient, error) {
	client, err := agentkgrpc.New(agentkclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("create agent-k gRPC client: %w", err)
	}
	return &SDKAgentKClient{client: client}, nil
}

func (c *SDKAgentKClient) CreateSession(
	ctx context.Context,
	title string,
	profile *apiv1.Profile,
) (string, error) {
	response, err := c.client.CreateSession(ctx, &apiv1.CreateSessionRequest{
		Title: title, Profile: profile,
	})
	if err != nil {
		return "", err
	}
	if response.GetSession() == nil ||
		!strings.HasPrefix(response.GetSession().GetId(), "ses_") {
		return "", fmt.Errorf("agent-k response is missing a valid ses_ id")
	}
	return response.GetSession().GetId(), nil
}

func (c *SDKAgentKClient) StartPipeline(
	ctx context.Context,
	sessionID string,
	args StoryboardArgs,
) (string, error) {
	input, err := structpb.NewStruct(map[string]any{
		"topic": args.Topic, "shots": args.Shots,
		"chatModel": args.ChatModel, "imageModel": args.ImageModel,
		"videoModel": args.VideoModel,
	})
	if err != nil {
		return "", fmt.Errorf("encode storyboard args: %w", err)
	}
	response, err := c.client.StartExecution(ctx, &apiv1.StartExecutionRequest{
		SessionId: sessionID,
		Input: &apiv1.StartExecutionRequest_Pipeline{
			Pipeline: &apiv1.PipelineExecutionInput{
				Pipeline: profilePipelineName,
				Args:     input,
			},
		},
	})
	if err != nil {
		return "", err
	}
	if response.GetExecution() == nil ||
		!strings.HasPrefix(response.GetExecution().GetId(), "exe_") {
		return "", fmt.Errorf("agent-k response is missing a valid exe_ id")
	}
	return response.GetExecution().GetId(), nil
}

func (c *SDKAgentKClient) GetPipelineRun(
	ctx context.Context,
	sessionID string,
	execID string,
) (PipelineSnapshot, error) {
	response, err := c.client.GetExecution(ctx, &apiv1.GetExecutionRequest{
		SessionId: sessionID, ExecutionId: execID,
	})
	if err != nil {
		return PipelineSnapshot{}, err
	}
	execution := response.GetExecution()
	if execution == nil {
		return PipelineSnapshot{}, fmt.Errorf("agent-k response is missing execution")
	}
	pipeline := execution.GetPipeline()
	if pipeline == nil {
		return PipelineSnapshot{}, fmt.Errorf("agent-k execution %q is not a pipeline", execID)
	}

	snapshot := PipelineSnapshot{
		CapabilityID: pipeline.GetCapabilityId(),
		Error:        execution.GetError(),
	}
	switch execution.GetStatus() {
	case apiv1.ExecutionStatus_EXECUTION_STATUS_RUNNING:
		snapshot.Status = string(JobRunning)
	case apiv1.ExecutionStatus_EXECUTION_STATUS_COMPLETED:
		snapshot.Status = string(JobCompleted)
		snapshot.Output, err = decodeStoryboardOutput(pipeline.GetOutput())
		if err != nil {
			return PipelineSnapshot{}, fmt.Errorf("decode pipeline output: %w", err)
		}
	case apiv1.ExecutionStatus_EXECUTION_STATUS_FAILED:
		snapshot.Status = string(JobFailed)
	case apiv1.ExecutionStatus_EXECUTION_STATUS_CANCELLED:
		snapshot.Status = string(JobFailed)
		if strings.TrimSpace(snapshot.Error) == "" {
			snapshot.Error = "pipeline cancelled"
		}
	default:
		return PipelineSnapshot{}, fmt.Errorf(
			"agent-k returned unknown execution status %q",
			execution.GetStatus(),
		)
	}
	return snapshot, nil
}

func decodeStoryboardOutput(value *structpb.Value) (*StoryboardOutput, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value.AsInterface())
	if err != nil {
		return nil, err
	}
	var output StoryboardOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil, err
	}
	return &output, nil
}
