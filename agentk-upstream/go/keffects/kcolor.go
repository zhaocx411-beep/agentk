package keffects

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// Published KColor 3.0 keffects workflows.
const (
	KColorTextToImageWorkflowName    = "kcolor_t2i_3.0"
	KColorReferenceImageWorkflowName = "kcolor_3.0"
)

// KColorMaxReferenceImages is the number of reference images kcolor_3.0 accepts.
const KColorMaxReferenceImages = 10

const kcolorMaxPromptRunes = 1000

var kcolorAspectRatios = map[string]bool{
	"9:16": true, "2:3": true, "3:4": true, "1:1": true, "4:3": true,
	"3:2": true, "16:9": true, "21:9": true, "auto": true,
}

var kcolorResolutions = map[string]bool{"1k": true, "2k": true, "4k": true}

// KColorTextToImageInput is the typed argument set of kcolor_t2i_3.0.
type KColorTextToImageInput struct {
	Prompt      string
	AspectRatio string
	Resolution  string
	// Style is omitted when empty.
	Style string
	// PromptJSON is a serialized JSON document; omitted when empty.
	PromptJSON string
}

// KColorReferenceImageInput is the typed argument set of kcolor_3.0.
type KColorReferenceImageInput struct {
	Prompt      string
	AspectRatio string
	Resolution  string
	// ReferenceImages holds 1 to 10 staged images.
	ReferenceImages []*pb.Media
}

// Validate checks the full kcolor_t2i_3.0 contract; the workflow takes no media.
func (in KColorTextToImageInput) Validate() error {
	if err := kcolorPrompt(in.Prompt); err != nil {
		return err
	}
	if err := kcolorDimensions(in.AspectRatio, in.Resolution); err != nil {
		return err
	}
	if in.PromptJSON != "" && !json.Valid([]byte(in.PromptJSON)) {
		return invalidInput("kcolor prompt_json must contain serialized JSON")
	}
	return nil
}

func (in KColorTextToImageInput) SubmitInput() (SubmitInput, error) {
	if err := in.Validate(); err != nil {
		return SubmitInput{}, err
	}
	arguments := []*pb.Argument{
		StringArgument("aspect_ratio", in.AspectRatio),
		StringArgument("resolution", in.Resolution),
		StringArgument("prompt", strings.TrimSpace(in.Prompt)),
	}
	if style := strings.TrimSpace(in.Style); style != "" {
		arguments = append(arguments, StringArgument("style", style))
	}
	if in.PromptJSON != "" {
		arguments = append(arguments, StringArgument("prompt_json", in.PromptJSON))
	}
	return SubmitInput{WorkflowName: KColorTextToImageWorkflowName, Arguments: arguments}, nil
}

// Validate checks the scalar kcolor_3.0 contract; reference images are checked
// by SubmitInput once they are staged.
func (in KColorReferenceImageInput) Validate() error {
	if err := kcolorPrompt(in.Prompt); err != nil {
		return err
	}
	return kcolorDimensions(in.AspectRatio, in.Resolution)
}

func (in KColorReferenceImageInput) SubmitInput() (SubmitInput, error) {
	if err := in.Validate(); err != nil {
		return SubmitInput{}, err
	}
	if err := requireMediaList("ref_image_list", in.ReferenceImages, 1, KColorMaxReferenceImages); err != nil {
		return SubmitInput{}, err
	}
	return SubmitInput{
		WorkflowName: KColorReferenceImageWorkflowName,
		Arguments: []*pb.Argument{
			StringArgument("aspect_ratio", in.AspectRatio),
			StringArgument("resolution", in.Resolution),
			MediaValuesArgument("ref_image_list", in.ReferenceImages),
			StringArgument("prompt", strings.TrimSpace(in.Prompt)),
		},
	}, nil
}

// ParseKColorImage returns the generated image shared by both KColor workflows.
func ParseKColorImage(response *pb.QueryResponse) (*pb.Media, error) {
	return FindMedia(response.GetOutputs(), "image")
}

func kcolorPrompt(prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return invalidInput("kcolor prompt is required")
	}
	if utf8.RuneCountInString(prompt) > kcolorMaxPromptRunes {
		return invalidInput("kcolor prompt must not exceed %d characters", kcolorMaxPromptRunes)
	}
	return nil
}

func kcolorDimensions(aspectRatio, resolution string) error {
	if !kcolorAspectRatios[aspectRatio] {
		return invalidInput("unsupported kcolor aspect ratio %q", aspectRatio)
	}
	if !kcolorResolutions[resolution] {
		return invalidInput("unsupported kcolor resolution %q", resolution)
	}
	return nil
}
