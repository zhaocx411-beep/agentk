package keffects

import (
	"strings"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// KlingVideoWorkflowName is the published keffects workflow that runs Kling
// text-to-video, image-to-video and reference-driven generation.
const KlingVideoWorkflowName = "kling_video"

// KlingVideoMaxRefVideos is the number of reference videos kling_video accepts.
const KlingVideoMaxRefVideos = 1

// KlingVideoInput is the typed argument set of the kling_video keffects
// workflow. All media must already be staged into the keffects work bucket.
type KlingVideoInput struct {
	Prompt   string
	Duration float64
	// Resolution is 720p or 1080p.
	Resolution string
	// AspectRatio is sent as given. When empty and the video follows a first
	// frame or reference video it is sent as "auto"; otherwise it is omitted.
	AspectRatio     string
	FirstFrame      *pb.Media
	TailImage       *pb.Media
	RefImages       []*pb.Media
	RefVideos       []*pb.Media
	SubjectLists    []string
	EnableAudio     bool
	EnableMultiShot bool
}

// Validate checks the parts of the contract that do not depend on media being
// staged, so callers can reject bad input before copying anything.
func (in KlingVideoInput) Validate() error {
	if strings.TrimSpace(in.Prompt) == "" {
		return invalidInput("kling_video prompt is required")
	}
	if in.Duration <= 0 {
		return invalidInput("kling_video duration must be positive")
	}
	if in.Resolution != "720p" && in.Resolution != "1080p" {
		return invalidInput("kling_video resolution must be 720p or 1080p, got %q", in.Resolution)
	}
	if len(in.RefVideos) > KlingVideoMaxRefVideos {
		return invalidInput("kling_video accepts at most %d reference video, got %d",
			KlingVideoMaxRefVideos, len(in.RefVideos))
	}
	return nil
}

// SubmitInput runs Validate, requires every media field to be staged, and
// builds the workflow arguments. RequestID is left for the caller.
func (in KlingVideoInput) SubmitInput() (SubmitInput, error) {
	if err := in.Validate(); err != nil {
		return SubmitInput{}, err
	}
	prompt := strings.TrimSpace(in.Prompt)
	if in.TailImage != nil && in.FirstFrame == nil {
		return SubmitInput{}, invalidInput("kling_video tail image requires a first frame")
	}
	if in.FirstFrame != nil {
		if err := requireMedia("first_frame", in.FirstFrame); err != nil {
			return SubmitInput{}, err
		}
	}
	if in.TailImage != nil {
		if err := requireMedia("tail_image", in.TailImage); err != nil {
			return SubmitInput{}, err
		}
	}
	if len(in.RefImages) > 0 {
		if err := requireMediaList("ref_images", in.RefImages, 1, len(in.RefImages)); err != nil {
			return SubmitInput{}, err
		}
	}
	if len(in.RefVideos) > 0 {
		if err := requireMediaList("ref_videos", in.RefVideos, 1, KlingVideoMaxRefVideos); err != nil {
			return SubmitInput{}, err
		}
	}

	arguments := []*pb.Argument{
		StringArgument("prompt", prompt),
		FloatArgument("duration", in.Duration),
		StringArgument("resolution", in.Resolution),
		BoolArgument("enable_audio", in.EnableAudio),
		BoolArgument("enable_multishot", in.EnableMultiShot),
	}
	aspectRatio := in.AspectRatio
	if aspectRatio == "" && (in.FirstFrame != nil || len(in.RefVideos) > 0) {
		aspectRatio = "auto"
	}
	if aspectRatio != "" {
		arguments = append(arguments, StringArgument("aspect_ratio", aspectRatio))
	}
	if in.FirstFrame != nil {
		arguments = append(arguments,
			MediaArgument("first_frame", in.FirstFrame.GetBucket(), in.FirstFrame.GetKey()))
	}
	if in.TailImage != nil {
		arguments = append(arguments,
			MediaArgument("tail_image", in.TailImage.GetBucket(), in.TailImage.GetKey()))
	}
	if len(in.RefImages) > 0 {
		arguments = append(arguments, MediaValuesArgument("ref_images", in.RefImages))
	}
	if len(in.SubjectLists) > 0 {
		arguments = append(arguments, StringValuesArgument("subject_lists", in.SubjectLists))
	}
	if len(in.RefVideos) > 0 {
		arguments = append(arguments,
			MediaValuesArgument("ref_videos", in.RefVideos),
			StringArgument("model_name", klingVideoReferenceModel(in.Resolution)),
		)
	}
	return SubmitInput{WorkflowName: KlingVideoWorkflowName, Arguments: arguments}, nil
}

// ParseKlingVideo returns the generated video from a successful response.
func ParseKlingVideo(response *pb.QueryResponse) (*pb.Media, error) {
	return FindMedia(response.GetOutputs(), "res_video")
}

// Reference-video generation runs on a dedicated model whose name encodes the
// output resolution; the workflow picks its own model in every other mode.
func klingVideoReferenceModel(resolution string) string {
	if resolution == "1080p" {
		return "Kling-v3_0_1-aio-omni-1080p-prod"
	}
	return "Kling-v3_0_1-aio-omni-720p-prod"
}
