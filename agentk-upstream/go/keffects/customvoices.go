package keffects

import (
	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// CustomVoicesWorkflowName is the published keffects workflow that extracts a
// reusable voice embedding from an audio sample.
const CustomVoicesWorkflowName = "custom_voices"

// CustomVoicesInput is the typed argument set of custom_voices.
type CustomVoicesInput struct {
	Audio *pb.Media
}

func (in CustomVoicesInput) SubmitInput() (SubmitInput, error) {
	if err := requireMedia("audio", in.Audio); err != nil {
		return SubmitInput{}, err
	}
	return SubmitInput{
		WorkflowName: CustomVoicesWorkflowName,
		Arguments: []*pb.Argument{
			MediaArgument("audio", in.Audio.GetBucket(), in.Audio.GetKey()),
		},
	}, nil
}

// ParseCustomVoices returns the voice embeddings produced by the workflow.
func ParseCustomVoices(response *pb.QueryResponse) ([]*pb.Media, error) {
	return FindMediaValues(response.GetOutputs(), "voice_embedding_list")
}
