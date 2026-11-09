package keffects

import (
	"strings"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// CustomElementsWorkflowName is the published keffects workflow that builds a
// reusable Kling subject from reference images or a reference video.
const CustomElementsWorkflowName = "advanced-custom-elements"

// CustomElementsInput is the typed argument set of advanced-custom-elements.
// The workflow runs in image mode when Video is nil and in video mode
// otherwise; FrontalImage is required in both modes.
type CustomElementsInput struct {
	Name        string
	Description string
	// FrontalImage is the primary reference in image mode and the frame that
	// identifies the subject in video mode.
	FrontalImage *pb.Media
	// ReferImages are additional references and are only valid in image mode.
	// When empty the frontal image is sent as the sole reference.
	ReferImages []*pb.Media
	// Video switches the workflow to video mode.
	Video *pb.Media
	// VoiceEmbeddings are optional custom_voices outputs bound to the subject.
	VoiceEmbeddings []*pb.Media
}

// CustomElementsOutput is the parsed result of advanced-custom-elements.
// SubjectList and SubjectLists are opaque selectors consumed by kling_video.
type CustomElementsOutput struct {
	SubjectList  string
	SubjectLists []string
	ReferImages  []*pb.Media
	ReferVideos  []*pb.Media
	ReferVoices  []*pb.Media
}

// Validate checks the scalar contract; media fields are checked by SubmitInput
// once they are staged.
func (in CustomElementsInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return invalidInput("advanced-custom-elements element_name is required")
	}
	return nil
}

func (in CustomElementsInput) SubmitInput() (SubmitInput, error) {
	if err := in.Validate(); err != nil {
		return SubmitInput{}, err
	}
	name := strings.TrimSpace(in.Name)
	if err := requireMedia("frontal_image", in.FrontalImage); err != nil {
		return SubmitInput{}, err
	}
	if in.Video != nil && len(in.ReferImages) > 0 {
		return SubmitInput{}, invalidInput(
			"advanced-custom-elements refer_images are not accepted together with a video")
	}
	arguments := []*pb.Argument{
		StringArgument("element_name", name),
		StringArgument("element_description", strings.TrimSpace(in.Description)),
	}
	if in.Video != nil {
		if err := requireMedia("video", in.Video); err != nil {
			return SubmitInput{}, err
		}
		arguments = append(arguments,
			StringArgument("reference_type", "VIDEO"),
			MediaArgument("video", in.Video.GetBucket(), in.Video.GetKey()),
			MediaArgument("frontal_image", in.FrontalImage.GetBucket(), in.FrontalImage.GetKey()),
		)
	} else {
		references := in.ReferImages
		if len(references) == 0 {
			references = []*pb.Media{in.FrontalImage}
		}
		if err := requireMediaList("refer_images", references, 1, len(references)); err != nil {
			return SubmitInput{}, err
		}
		arguments = append(arguments,
			StringArgument("reference_type", "IMAGE"),
			MediaArgument("frontal_image", in.FrontalImage.GetBucket(), in.FrontalImage.GetKey()),
			MediaValuesArgument("refer_images", references),
		)
	}
	if len(in.VoiceEmbeddings) > 0 {
		if err := requireMediaList("voice_embeddings", in.VoiceEmbeddings, 1, len(in.VoiceEmbeddings)); err != nil {
			return SubmitInput{}, err
		}
		arguments = append(arguments, MediaValuesArgument("voice_embeddings", in.VoiceEmbeddings))
	}
	return SubmitInput{WorkflowName: CustomElementsWorkflowName, Arguments: arguments}, nil
}

// ParseCustomElements reads the subject selectors and the optional reference
// media the workflow republishes.
func ParseCustomElements(response *pb.QueryResponse) (CustomElementsOutput, error) {
	outputs := response.GetOutputs()
	subjectList, err := FindString(outputs, "subject_list")
	if err != nil {
		return CustomElementsOutput{}, err
	}
	subjectLists, err := FindStringValues(outputs, "subject_lists")
	if err != nil {
		return CustomElementsOutput{}, err
	}
	return CustomElementsOutput{
		SubjectList:  subjectList,
		SubjectLists: subjectLists,
		ReferImages:  optionalMediaValues(outputs, "refer_images"),
		ReferVideos:  optionalMediaValues(outputs, "refer_videos"),
		ReferVoices:  optionalMediaValues(outputs, "refer_voices"),
	}, nil
}
