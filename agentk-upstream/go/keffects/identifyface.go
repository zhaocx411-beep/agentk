package keffects

import (
	"fmt"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

// IdentifyFaceWorkflowName is the published keffects workflow that detects the
// faces in a video and returns the selectors advanced-lip-sync needs.
const IdentifyFaceWorkflowName = "identify-face"

// IdentifyFaceInput is the typed argument set of identify-face.
type IdentifyFaceInput struct {
	Video *pb.Media
}

// IdentifyFaceOutput is the parsed result of identify-face. FaceIDs and
// FaceImages are aligned by index.
type IdentifyFaceOutput struct {
	FirstFaceID  string
	FaceIndexKey string
	FaceIDs      []string
	FaceImages   []*pb.Media
}

func (in IdentifyFaceInput) SubmitInput() (SubmitInput, error) {
	if err := requireMedia("video", in.Video); err != nil {
		return SubmitInput{}, err
	}
	return SubmitInput{
		WorkflowName: IdentifyFaceWorkflowName,
		Arguments: []*pb.Argument{
			MediaArgument("video", in.Video.GetBucket(), in.Video.GetKey()),
		},
	}, nil
}

func ParseIdentifyFace(response *pb.QueryResponse) (IdentifyFaceOutput, error) {
	outputs := response.GetOutputs()
	firstFaceID, err := FindString(outputs, "first_face_id")
	if err != nil {
		return IdentifyFaceOutput{}, err
	}
	faceIndexKey, err := FindString(outputs, "face_index_key")
	if err != nil {
		return IdentifyFaceOutput{}, err
	}
	faceIDs, err := FindStringValues(outputs, "face_id_list")
	if err != nil {
		return IdentifyFaceOutput{}, err
	}
	faceImages, err := FindMediaValues(outputs, "face_image_list")
	if err != nil {
		return IdentifyFaceOutput{}, err
	}
	if len(faceIDs) != len(faceImages) {
		return IdentifyFaceOutput{}, fmt.Errorf(
			"keffects: identify-face returned %d face ids but %d face images",
			len(faceIDs), len(faceImages))
	}
	return IdentifyFaceOutput{
		FirstFaceID: firstFaceID, FaceIndexKey: faceIndexKey,
		FaceIDs: faceIDs, FaceImages: faceImages,
	}, nil
}
