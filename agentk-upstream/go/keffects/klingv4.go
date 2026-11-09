package keffects

import (
	"errors"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

const (
	KlingV4WorkflowName      = "kling_v4"
	KlingV4FlashWorkflowName = "kling_v4_flash"
)

type KlingV4Input struct {
	Prompt       string
	Duration     float64
	AspectRatio  string
	EnableIntent bool
	FirstFrame   *pb.Media
	TailFrame    *pb.Media
	ReferImages  []*pb.Media
	BaseVideos   []*pb.Media
	ReferVideos  []*pb.Media
	SubjectList  string
	Resolution   string
	MiddleFrames []*pb.Media
	ReferVoices  []*pb.Media
	PrePE        *bool
}

type KlingV4FlashInput struct {
	Prompt       string
	Duration     float64
	AspectRatio  string
	EnableIntent bool
	FirstFrame   *pb.Media
	TailFrame    *pb.Media
	ReferImages  []*pb.Media
	BaseVideos   []*pb.Media
	ReferVideos  []*pb.Media
	SubjectList  string
}

func (in KlingV4Input) SubmitInput() (SubmitInput, error) {
	arguments := []*pb.Argument{BoolArgument("enable_intent", in.EnableIntent)}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"prompt", in.Prompt},
		{"aspect_ratio", in.AspectRatio},
		{"subject_list", in.SubjectList},
		{"resolution", in.Resolution},
	} {
		if field.value != "" {
			arguments = append(arguments, StringArgument(field.name, field.value))
		}
	}
	if in.Duration != 0 {
		arguments = append(arguments, FloatArgument("duration", in.Duration))
	}
	if in.PrePE != nil {
		arguments = append(arguments, BoolArgument("pre_pe", *in.PrePE))
	}
	for _, field := range []struct {
		name  string
		value *pb.Media
	}{
		{"first_frame", in.FirstFrame},
		{"tail_frame", in.TailFrame},
	} {
		if field.value == nil {
			continue
		}
		if err := requireMedia(field.name, field.value); err != nil {
			return SubmitInput{}, err
		}
		arguments = append(arguments, MediaArgument(field.name, field.value.GetBucket(), field.value.GetKey()))
	}
	for _, field := range []struct {
		name  string
		value []*pb.Media
	}{
		{"middle_frames", in.MiddleFrames},
		{"refer_images", in.ReferImages},
		{"base_videos", in.BaseVideos},
		{"refer_videos", in.ReferVideos},
		{"refer_voices", in.ReferVoices},
	} {
		if len(field.value) == 0 {
			continue
		}
		if err := requireMediaList(field.name, field.value, 1, len(field.value)); err != nil {
			return SubmitInput{}, err
		}
		arguments = append(arguments, MediaValuesArgument(field.name, field.value))
	}
	return SubmitInput{WorkflowName: KlingV4WorkflowName, Arguments: arguments}, nil
}

func (in KlingV4FlashInput) SubmitInput() (SubmitInput, error) {
	submit, err := (KlingV4Input{
		Prompt: in.Prompt, Duration: in.Duration, AspectRatio: in.AspectRatio,
		EnableIntent: in.EnableIntent, FirstFrame: in.FirstFrame, TailFrame: in.TailFrame,
		ReferImages: in.ReferImages, BaseVideos: in.BaseVideos, ReferVideos: in.ReferVideos,
		SubjectList: in.SubjectList,
	}).SubmitInput()
	if err != nil {
		return SubmitInput{}, err
	}
	submit.WorkflowName = KlingV4FlashWorkflowName
	return submit, nil
}

type KlingV4Output struct {
	Video            *pb.Media
	WatermarkedVideo *pb.Media
	Cover            *pb.Media
	WatermarkedCover *pb.Media
}

func ParseKlingV4(response *pb.QueryResponse) (KlingV4Output, error) {
	outputs := response.GetOutputs()
	video, err := FindMedia(outputs, "video")
	if err != nil {
		return KlingV4Output{}, err
	}
	result := KlingV4Output{Video: video}
	for _, field := range []struct {
		name  string
		value **pb.Media
	}{
		{"wm_video", &result.WatermarkedVideo},
		{"cover", &result.Cover},
		{"wm_cover", &result.WatermarkedCover},
	} {
		media, err := FindMedia(outputs, field.name)
		if errors.Is(err, ErrOutputMissing) {
			continue
		}
		if err != nil {
			return KlingV4Output{}, err
		}
		*field.value = media
	}
	return result, nil
}
