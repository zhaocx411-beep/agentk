package keffects

import (
	"strings"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// LipSyncWorkflowName is the published keffects workflow that re-times a
// speaker's mouth in a video to a replacement audio track.
const LipSyncWorkflowName = "advanced-lip-sync"

// LipSyncInput is the typed argument set of advanced-lip-sync. FaceIndexKey and
// FaceID come from identify-face; sound times are milliseconds.
type LipSyncInput struct {
	Video               *pb.Media
	Audio               *pb.Media
	FaceIndexKey        string
	FaceID              string
	SoundInsertTime     int64
	SoundStartTime      int64
	SoundEndTime        int64
	SoundVolume         float64
	OriginalAudioVolume float64
}

// Validate checks the scalar contract; video and audio are checked by
// SubmitInput once they are staged.
func (in LipSyncInput) Validate() error {
	if strings.TrimSpace(in.FaceIndexKey) == "" || strings.TrimSpace(in.FaceID) == "" {
		return invalidInput("advanced-lip-sync face_id_key and face_id are required")
	}
	if in.SoundInsertTime < 0 || in.SoundStartTime < 0 || in.SoundEndTime <= in.SoundStartTime {
		return invalidInput(
			"advanced-lip-sync sound times must be non-negative and sound_end_time must follow sound_start_time")
	}
	if in.SoundVolume < 0 || in.OriginalAudioVolume < 0 {
		return invalidInput("advanced-lip-sync volumes must not be negative")
	}
	return nil
}

func (in LipSyncInput) SubmitInput() (SubmitInput, error) {
	if err := in.Validate(); err != nil {
		return SubmitInput{}, err
	}
	if err := requireMedia("video", in.Video); err != nil {
		return SubmitInput{}, err
	}
	if err := requireMedia("audio", in.Audio); err != nil {
		return SubmitInput{}, err
	}
	faceIndexKey := strings.TrimSpace(in.FaceIndexKey)
	faceID := strings.TrimSpace(in.FaceID)
	return SubmitInput{
		WorkflowName: LipSyncWorkflowName,
		Arguments: []*pb.Argument{
			MediaArgument("video", in.Video.GetBucket(), in.Video.GetKey()),
			MediaArgument("audio", in.Audio.GetBucket(), in.Audio.GetKey()),
			StringArgument("face_id_key", faceIndexKey),
			StringArgument("face_id", faceID),
			Int64Argument("sound_insert_time", in.SoundInsertTime),
			Int64Argument("sound_start_time", in.SoundStartTime),
			Int64Argument("sound_end_time", in.SoundEndTime),
			FloatArgument("sound_volume", in.SoundVolume),
			FloatArgument("original_audio_volume", in.OriginalAudioVolume),
		},
	}, nil
}

// ParseLipSync returns the re-synced video.
func ParseLipSync(response *pb.QueryResponse) (*pb.Media, error) {
	return FindMedia(response.GetOutputs(), "video")
}
