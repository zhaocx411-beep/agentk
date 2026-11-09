package main

import (
	"encoding/json"
	"fmt"
	"strings"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	defaultShots = 3
	maxShots     = 8
)

func StoryboardFilmWorkflow(ctx workflow.Context, input StoryboardInput) (StoryboardOutput, error) {
	input, err := normalizeInput(input)
	if err != nil {
		return StoryboardOutput{}, err
	}

	shots, err := generateBoard(ctx, input)
	if err != nil {
		return StoryboardOutput{}, err
	}
	reportProgress(ctx, "board", fmt.Sprintf("分镜完成，共 %d 镜", len(shots)))

	clipURLs := make([]string, len(shots))
	clipErrs := make([]error, len(shots))
	group := workflow.NewWaitGroup(ctx)
	for i, shot := range shots {
		i, shot := i, shot
		group.Add(1)
		workflow.Go(ctx, func(gctx workflow.Context) {
			defer group.Done()
			clipURLs[i], clipErrs[i] = generateClip(gctx, input, i, shot)
			if clipErrs[i] == nil {
				reportProgress(gctx, fmt.Sprintf("clip/%d", i), "片段完成")
			}
		})
	}
	group.Wait(ctx)

	successful := make([]string, 0, len(clipURLs))
	for _, url := range clipURLs {
		if url != "" {
			successful = append(successful, url)
		}
	}
	if len(successful) == 0 {
		for _, clipErr := range clipErrs {
			if clipErr != nil {
				return StoryboardOutput{}, fmt.Errorf("storyboard: all clips failed: %w", clipErr)
			}
		}
		return StoryboardOutput{}, fmt.Errorf("storyboard: all clips failed")
	}

	var composed concatVideosOutput
	if err := sdkcapability.Invoke(ctx, composeVideoCapability, concatVideosInput{
		VideoURLs: successful,
	}, &composed); err != nil {
		return StoryboardOutput{}, err
	}
	if len(composed.Artifacts) == 0 || composed.Artifacts[0].URI == "" {
		return StoryboardOutput{}, fmt.Errorf("storyboard: concat_videos returned no artifacts")
	}
	composedArtifact := composed.Artifacts[0]
	return StoryboardOutput{
		Artifacts: []sdkcapability.Artifact{{
			Type:        "video",
			URI:         composedArtifact.URI,
			Description: fmt.Sprintf("围绕「%s」生成的分镜短片，共 %d 个片段。", input.Topic, composed.ClipCount),
			Model:       input.VideoModel,
			Params:      composedArtifact.Params,
		}},
		ClipCount: composed.ClipCount,
	}, nil
}

func normalizeInput(input StoryboardInput) (StoryboardInput, error) {
	input.Topic = strings.TrimSpace(input.Topic)
	input.ChatModel = strings.TrimSpace(input.ChatModel)
	input.ImageModel = strings.TrimSpace(input.ImageModel)
	input.VideoModel = strings.TrimSpace(input.VideoModel)
	if input.Topic == "" || input.ChatModel == "" || input.ImageModel == "" || input.VideoModel == "" {
		return StoryboardInput{}, temporal.NewNonRetryableApplicationError(
			"storyboard: topic, chatModel, imageModel and videoModel are required",
			"InvalidArgument",
			nil,
		)
	}
	if input.Shots == 0 {
		input.Shots = defaultShots
	}
	if input.Shots < 1 || input.Shots > maxShots {
		return StoryboardInput{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("storyboard: shots must be between 1 and %d", maxShots),
			"InvalidArgument",
			nil,
		)
	}
	return input, nil
}

func generateBoard(ctx workflow.Context, input StoryboardInput) ([]storyboardShot, error) {
	var output generateTextOutput
	if err := sdkcapability.Invoke(ctx, generateTextCapability, generateTextInput{
		Model: input.ChatModel,
		Prompt: fmt.Sprintf(
			"围绕「%s」设计 %d 个连贯镜头。\n每个镜头包含 frame（静态首帧描述）和 motion（运动描述）。",
			input.Topic,
			input.Shots,
		),
		OutputSchema: sdkcapability.SchemaOf[storyboardBoard](),
	}, &output); err != nil {
		return nil, err
	}

	var board storyboardBoard
	if err := json.Unmarshal(output.Result, &board); err != nil {
		return nil, fmt.Errorf("storyboard: decode board: %w", err)
	}
	if len(board.Shots) == 0 {
		return nil, fmt.Errorf("storyboard: board returned no shots")
	}
	if len(board.Shots) > input.Shots {
		board.Shots = board.Shots[:input.Shots]
	}
	return board.Shots, nil
}

func generateClip(
	ctx workflow.Context,
	input StoryboardInput,
	index int,
	shot storyboardShot,
) (string, error) {
	var frame generateImageOutput
	if err := sdkcapability.Invoke(ctx, generateImageCapability, generateImageInput{
		Model:       input.ImageModel,
		Prompt:      shot.Frame,
		AspectRatio: "16:9",
	}, &frame); err != nil {
		return "", err
	}
	if len(frame.Artifacts) == 0 || frame.Artifacts[0].URI == "" {
		return "", fmt.Errorf("storyboard: shot %d: generate_image returned no artifacts", index)
	}

	var clip generateVideoOutput
	if err := sdkcapability.Invoke(ctx, generateVideoCapability, generateVideoInput{
		Model:      input.VideoModel,
		FirstFrame: frame.Artifacts[0].URI,
		Prompt:     shot.Motion,
	}, &clip); err != nil {
		return "", err
	}
	if len(clip.Artifacts) == 0 || clip.Artifacts[0].URI == "" {
		return "", fmt.Errorf("storyboard: shot %d: generate_video returned no artifacts", index)
	}
	return clip.Artifacts[0].URI, nil
}

func reportProgress(ctx workflow.Context, stepID, message string) {
	if err := invocation.ReportProgress(ctx, stepID, message, nil); err != nil {
		workflow.GetLogger(ctx).Warn(
			"storyboard example: report progress failed",
			"stepId",
			stepID,
			"error",
			err,
		)
	}
}
