package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestStoryboardFilmWorkflow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerCapabilityResolver(env)
	env.RegisterWorkflowWithOptions(
		StoryboardFilmWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)

	var textInput generateTextInput
	var imageInput generateImageInput
	var videoInput generateVideoInput
	var composeInput concatVideosInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateTextInput) (generateTextOutput, error) {
			textInput = input
			return generateTextOutput{
				Result: json.RawMessage(`{"shots":[{"frame":"猫在雨夜","motion":"猫向前走"}]}`),
			}, nil
		},
		activity.RegisterOptions{Name: generateTextCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateImageInput) (generateImageOutput, error) {
			imageInput = input
			return generateImageOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "image",
				URI:  "https://image/frame.png",
			}}}, nil
		},
		activity.RegisterOptions{Name: generateImageCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateVideoInput) (generateVideoOutput, error) {
			videoInput = input
			return generateVideoOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "video",
				URI:  "https://video/clip.mp4",
			}}}, nil
		},
		activity.RegisterOptions{Name: generateVideoCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input concatVideosInput) (concatVideosOutput, error) {
			composeInput = input
			return concatVideosOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "video",
					URI:  "https://video/film.mp4",
				}},
				ClipCount: len(input.VideoURLs),
			}, nil
		},
		activity.RegisterOptions{Name: composeVideoCapability},
	)

	env.ExecuteWorkflow(WorkflowType, StoryboardInput{
		Topic:      "雨夜的猫",
		ChatModel:  "chat-cap",
		ImageModel: "image-cap",
		VideoModel: "video-cap",
	})
	require.NoError(t, env.GetWorkflowError())

	var output StoryboardOutput
	require.NoError(t, env.GetWorkflowResult(&output))
	require.Equal(t, StoryboardOutput{
		Artifacts: []sdkcapability.Artifact{{
			Type:        "video",
			URI:         "https://video/film.mp4",
			Description: "围绕「雨夜的猫」生成的分镜短片，共 1 个片段。",
			Model:       "video-cap",
		}},
		ClipCount: 1,
	}, output)
	require.Equal(t, "chat-cap", textInput.Model)
	require.Contains(t, textInput.Prompt, "3 个连贯镜头")
	require.NotEmpty(t, textInput.OutputSchema)
	require.Equal(t, generateImageInput{
		Model:       "image-cap",
		Prompt:      "猫在雨夜",
		AspectRatio: "16:9",
	}, imageInput)
	require.Equal(t, generateVideoInput{
		Model:      "video-cap",
		FirstFrame: "https://image/frame.png",
		Prompt:     "猫向前走",
	}, videoInput)
	require.Equal(t, []string{"https://video/clip.mp4"}, composeInput.VideoURLs)
}

func TestPublishedInputSchemaConstrainsShots(t *testing.T) {
	schema := string(sdkcapability.SchemaOf[StoryboardInput]())
	require.Contains(t, schema, `"minimum":1`)
	require.Contains(t, schema, `"maximum":8`)
	require.Contains(t, schema, `"additionalProperties":false`)
}

func TestStoryboardFilmWorkflowAllowsPartialClipFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerCapabilityResolver(env)
	env.RegisterWorkflowWithOptions(
		StoryboardFilmWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)

	env.RegisterActivityWithOptions(
		func(context.Context, generateTextInput) (generateTextOutput, error) {
			return generateTextOutput{Result: json.RawMessage(`{
				"shots": [
					{"frame":"frame-0","motion":"motion-0"},
					{"frame":"frame-1","motion":"motion-1"},
					{"frame":"frame-2","motion":"motion-2"}
				]
			}`)}, nil
		},
		activity.RegisterOptions{Name: generateTextCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateImageInput) (generateImageOutput, error) {
			return generateImageOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "image",
				URI:  "https://image/" + input.Prompt,
			}}}, nil
		},
		activity.RegisterOptions{Name: generateImageCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateVideoInput) (generateVideoOutput, error) {
			if input.Prompt == "motion-1" {
				return generateVideoOutput{}, temporal.NewNonRetryableApplicationError(
					"shot rejected",
					"UpstreamRequestRejected",
					nil,
				)
			}
			return generateVideoOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "video",
				URI:  "https://video/" + input.Prompt,
			}}}, nil
		},
		activity.RegisterOptions{Name: generateVideoCapability},
	)

	var composedURLs []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input concatVideosInput) (concatVideosOutput, error) {
			composedURLs = append([]string(nil), input.VideoURLs...)
			return concatVideosOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "video",
					URI:  "https://video/film.mp4",
				}},
				ClipCount: len(input.VideoURLs),
			}, nil
		},
		activity.RegisterOptions{Name: composeVideoCapability},
	)

	env.ExecuteWorkflow(WorkflowType, validInput(3))
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{
		"https://video/motion-0",
		"https://video/motion-2",
	}, composedURLs)
}

func TestStoryboardFilmWorkflowCapsModelShotsToRequestedCount(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerCapabilityResolver(env)
	env.RegisterWorkflowWithOptions(
		StoryboardFilmWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, generateTextInput) (generateTextOutput, error) {
			return generateTextOutput{Result: json.RawMessage(`{
				"shots": [
					{"frame":"frame-0","motion":"motion-0"},
					{"frame":"frame-1","motion":"motion-1"}
				]
			}`)}, nil
		},
		activity.RegisterOptions{Name: generateTextCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateImageInput) (generateImageOutput, error) {
			return generateImageOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "image",
				URI:  "https://image/" + input.Prompt,
			}}}, nil
		},
		activity.RegisterOptions{Name: generateImageCapability},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input generateVideoInput) (generateVideoOutput, error) {
			return generateVideoOutput{Artifacts: []sdkcapability.Artifact{{
				Type: "video",
				URI:  "https://video/" + input.Prompt,
			}}}, nil
		},
		activity.RegisterOptions{Name: generateVideoCapability},
	)
	var composedURLs []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input concatVideosInput) (concatVideosOutput, error) {
			composedURLs = append([]string(nil), input.VideoURLs...)
			return concatVideosOutput{
				Artifacts: []sdkcapability.Artifact{{
					Type: "video",
					URI:  "https://video/film.mp4",
				}},
				ClipCount: len(input.VideoURLs),
			}, nil
		},
		activity.RegisterOptions{Name: composeVideoCapability},
	)

	env.ExecuteWorkflow(WorkflowType, validInput(1))
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, []string{"https://video/motion-0"}, composedURLs)
}

func TestStoryboardFilmWorkflowFailsWhenAllClipsFail(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerCapabilityResolver(env)
	env.RegisterWorkflowWithOptions(
		StoryboardFilmWorkflow,
		workflow.RegisterOptions{Name: WorkflowType},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, generateTextInput) (generateTextOutput, error) {
			return generateTextOutput{
				Result: json.RawMessage(`{"shots":[{"frame":"frame","motion":"motion"}]}`),
			}, nil
		},
		activity.RegisterOptions{Name: generateTextCapability},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, generateImageInput) (generateImageOutput, error) {
			return generateImageOutput{}, temporal.NewNonRetryableApplicationError(
				"image rejected",
				"UpstreamRequestRejected",
				nil,
			)
		},
		activity.RegisterOptions{Name: generateImageCapability},
	)
	composeCalled := false
	env.RegisterActivityWithOptions(
		func(context.Context, concatVideosInput) (concatVideosOutput, error) {
			composeCalled = true
			return concatVideosOutput{}, nil
		},
		activity.RegisterOptions{Name: composeVideoCapability},
	)

	env.ExecuteWorkflow(WorkflowType, validInput(1))
	require.Error(t, env.GetWorkflowError())
	require.Contains(t, env.GetWorkflowError().Error(), "all clips failed")
	require.False(t, composeCalled)
}

func TestStoryboardFilmWorkflowRejectsInvalidInput(t *testing.T) {
	tests := map[string]StoryboardInput{
		"missing topic": {
			ChatModel: "chat", ImageModel: "image", VideoModel: "video",
		},
		"shots below range": {
			Topic: "topic", Shots: -1,
			ChatModel: "chat", ImageModel: "image", VideoModel: "video",
		},
		"shots above range": {
			Topic: "topic", Shots: 9,
			ChatModel: "chat", ImageModel: "image", VideoModel: "video",
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterWorkflowWithOptions(
				StoryboardFilmWorkflow,
				workflow.RegisterOptions{Name: WorkflowType},
			)
			env.ExecuteWorkflow(WorkflowType, input)
			err := env.GetWorkflowError()
			require.Error(t, err)
			require.Contains(t, err.Error(), "InvalidArgument")
		})
	}
}

func TestCoreCapabilityPayloadsContainOnlyBusinessFields(t *testing.T) {
	payloads := []any{
		generateTextInput{},
		generateImageInput{},
		generateVideoInput{},
		concatVideosInput{},
	}
	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(raw, &fields))
		for _, forbidden := range []string{
			"session",
			"sessionId",
			"tenant",
			"tenantId",
			"execPublicId",
		} {
			require.NotContains(t, fields, forbidden)
		}
	}
}

func validInput(shots int) StoryboardInput {
	return StoryboardInput{
		Topic:      "topic",
		Shots:      shots,
		ChatModel:  "chat",
		ImageModel: "image",
		VideoModel: "video",
	}
}

type testResolveInput struct {
	CapabilityID string `json:"capabilityId"`
}

type testRoutingTarget struct {
	Kind           string `json:"kind"`
	TemporalType   string `json:"temporalType"`
	TaskQueue      string `json:"taskQueue"`
	TimeoutSeconds int64  `json:"timeoutSeconds"`
}

func registerCapabilityResolver(env *testsuite.TestWorkflowEnvironment) {
	targets := map[string]string{
		generateTextCapability:  generateTextCapability,
		generateImageCapability: generateImageCapability,
		generateVideoCapability: generateVideoCapability,
		composeVideoCapability:  composeVideoCapability,
	}
	env.RegisterActivityWithOptions(
		func(_ context.Context, input testResolveInput) (testRoutingTarget, error) {
			temporalType, ok := targets[input.CapabilityID]
			if !ok {
				return testRoutingTarget{}, fmt.Errorf(
					"unknown capability %q", input.CapabilityID)
			}
			return testRoutingTarget{
				Kind: "activity", TemporalType: temporalType,
				TaskQueue: "agentk", TimeoutSeconds: 24 * 60 * 60,
			}, nil
		},
		activity.RegisterOptions{Name: "agentk.routing.resolve.v1"},
	)
}

func TestBoardSchemaUsesBoardContract(t *testing.T) {
	schema := sdkcapability.SchemaOf[storyboardBoard]()
	require.True(t, json.Valid(schema))
	require.NotContains(t, string(schema), "sessionId")
}
