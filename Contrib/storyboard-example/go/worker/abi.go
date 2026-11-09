package main

import (
	"encoding/json"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	CapabilityID    = "contrib.storyboard_example.storyboard_film_go"
	WorkflowType    = "StoryboardFilmExample"
	WorkerTaskQueue = "contrib.storyboard-example-go"

	generateTextCapability  = "agentk.generate_text"
	generateImageCapability = "agentk.generate_image"
	generateVideoCapability = "agentk.generate_video"
	composeVideoCapability  = "agentk.compose_video"
)

type StoryboardInput struct {
	Topic      string `json:"topic" jsonschema:"minLength=1,description=短片主题"`
	Shots      int    `json:"shots,omitempty" jsonschema:"minimum=1,maximum=8,description=镜头数，缺省 3"`
	ChatModel  string `json:"chatModel" jsonschema:"minLength=1,description=分镜文案模型"`
	ImageModel string `json:"imageModel" jsonschema:"minLength=1,description=首帧图模型"`
	VideoModel string `json:"videoModel" jsonschema:"minLength=1,description=视频片段模型"`
}

type StoryboardOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
	ClipCount int                      `json:"clipCount"`
}

type storyboardShot struct {
	Frame  string `json:"frame"`
	Motion string `json:"motion"`
}

type storyboardBoard struct {
	Shots []storyboardShot `json:"shots"`
}

type generateTextInput struct {
	Model        string          `json:"model"`
	Prompt       string          `json:"prompt"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type generateTextOutput struct {
	Result json.RawMessage `json:"result"`
}

type generateImageInput struct {
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspectRatio,omitempty"`
}

type generateImageOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
}

type generateVideoInput struct {
	Model      string `json:"model"`
	FirstFrame string `json:"firstFrame"`
	LastFrame  string `json:"lastFrame,omitempty"`
	Prompt     string `json:"prompt"`
	Resolution string `json:"resolution,omitempty"`
	Duration   string `json:"duration,omitempty"`
	Audio      string `json:"audio,omitempty"`
}

type generateVideoOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
}

type concatVideosInput struct {
	VideoURLs []string `json:"videoUrls"`
}

type concatVideosOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
	ClipCount int                      `json:"clipCount"`
}
