package videogen

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/keffects"

	"agentk/internal/artifact"
)

type fakeKeffectsWorkflow struct {
	submitInput keffects.SubmitInput
	instanceID  int64
	query       *pb.QueryResponse
	stageBucket string
	outputURI   string
}

func (f *fakeKeffectsWorkflow) Submit(_ context.Context, in keffects.SubmitInput) (int64, error) {
	f.submitInput = in
	return f.instanceID, nil
}

func (f *fakeKeffectsWorkflow) QueryResult(context.Context, int64) (*pb.QueryResponse, error) {
	return f.query, nil
}

func (f *fakeKeffectsWorkflow) StageMedia(
	_ context.Context,
	uri string,
	_ keffects.StageMediaOptions,
) (*pb.Media, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	key := strings.TrimPrefix(parsed.Path, "/")
	if slash := strings.IndexByte(key, '/'); slash >= 0 {
		key = key[slash+1:]
	}
	bucket := f.stageBucket
	if bucket == "" {
		bucket = "agentk-test"
	}
	return &pb.Media{Bucket: bucket, Key: key}, nil
}

func (f *fakeKeffectsWorkflow) OutputURI(media *pb.Media) (string, error) {
	if f.outputURI != "" {
		return f.outputURI, nil
	}
	if media == nil {
		return "", fmt.Errorf("empty media")
	}
	return "https://cdn.test/" + media.GetBucket() + "/" + media.GetKey(), nil
}

func newWorkflowTestStorage(t *testing.T) artifact.Storage {
	t.Helper()
	return artifact.NewLocalStorage(t.TempDir(), "http://local.test")
}

func TestKlingWorkflowT2VMapsArgumentsAndOutput(t *testing.T) {
	rpc := &fakeKeffectsWorkflow{
		instanceID: 12345,
		outputURI:  "https://cdn.test/agentk-test/outputs/t2v.mp4",
		query: &pb.QueryResponse{Status: keffects.CodeSuccess, Outputs: []*pb.Argument{
			keffects.MediaArgument("res_video", "agentk-test", "outputs/t2v.mp4"),
		}},
	}
	client := NewKlingWorkflow(newWorkflowTestStorage(t), rpc).T2V()
	multiShot := true
	taskID, err := client.Submit(context.Background(), KlingT2VRequest{
		Prompt: "夏日海边，海浪拍打礁石", Resolution: "720p", Duration: "5",
		AspectRatio: "16:9", Audio: "native", MultiShot: &multiShot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "12345" ||
		rpc.submitInput.WorkflowName != klingWorkflowName ||
		rpc.submitInput.OutputBucketName != "" {
		t.Fatalf("task=%q submit=%+v", taskID, rpc.submitInput)
	}
	args := argumentsByName(rpc.submitInput.Arguments)
	if args["prompt"].GetStringValue() != "夏日海边，海浪拍打礁石" ||
		args["duration"].GetFloatValue() != 5 ||
		args["resolution"].GetStringValue() != "720p" ||
		args["aspect_ratio"].GetStringValue() != "16:9" ||
		!args["enable_audio"].GetBoolValue() ||
		!args["enable_multishot"].GetBoolValue() {
		t.Fatalf("arguments = %+v", args)
	}
	if _, ok := args["first_frame"]; ok {
		t.Fatal("text-to-video must omit first_frame")
	}

	result, err := client.Poll(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Done || string(result.Result) !=
		`{"artifacts":[{"type":"video","uri":"https://cdn.test/agentk-test/outputs/t2v.mp4"}]}` {
		t.Fatalf("poll result = %+v", result)
	}
}

func TestKlingWorkflowI2VMapsStagedFirstFrame(t *testing.T) {
	storage := newWorkflowTestStorage(t)
	frame, err := storage.Put(context.Background(), "inputs/frame.png", []byte("png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeKeffectsWorkflow{instanceID: 8}
	client := NewKlingWorkflow(storage, rpc).I2V()
	multiShot := false
	if _, err := client.Submit(context.Background(), KlingI2VRequest{
		FirstFrame: frame.URL, Prompt: "人物缓缓转身", Resolution: "1080p",
		Duration: "5", Audio: "off", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	args := argumentsByName(rpc.submitInput.Arguments)
	if args["aspect_ratio"].GetStringValue() != "auto" {
		t.Fatalf("aspect_ratio = %q", args["aspect_ratio"].GetStringValue())
	}
	first := args["first_frame"].GetMediaValue()
	if first.GetBucket() != "agentk-test" || first.GetKey() != "inputs/frame.png" {
		t.Fatalf("first_frame = %+v", first)
	}
}

func TestKlingWorkflowOmniReferenceVideoSelectsV301Model(t *testing.T) {
	storage := newWorkflowTestStorage(t)
	video, err := storage.Put(context.Background(), "inputs/reference.mp4", []byte("mp4"), "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeKeffectsWorkflow{instanceID: 9}
	client := NewKlingWorkflow(storage, rpc).Omni()
	multiShot := false
	if _, err := client.Submit(context.Background(), KlingOmniRequest{
		Prompt: "参考运镜", FeatureVideo: video.URL, Resolution: "1080p",
		Duration: "5", Audio: "off", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	args := argumentsByName(rpc.submitInput.Arguments)
	if args["model_name"].GetStringValue() != "Kling-v3_0_1-aio-omni-1080p-prod" {
		t.Fatalf("model_name = %q", args["model_name"].GetStringValue())
	}
	if args["aspect_ratio"].GetStringValue() != "auto" {
		t.Fatalf("aspect_ratio = %q", args["aspect_ratio"].GetStringValue())
	}
	refs := args["ref_videos"].GetMediaValues().GetValue()
	if len(refs) != 1 || refs[0].GetBucket() != "agentk-test" ||
		refs[0].GetKey() != "inputs/reference.mp4" {
		t.Fatalf("ref_videos = %+v", refs)
	}
}

func TestKlingWorkflowOmniReferenceImagesOmitFirstFrameAndSelectV30Model(t *testing.T) {
	storage := newWorkflowTestStorage(t)
	reference, err := storage.Put(context.Background(), "inputs/reference.png", []byte("png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	rpc := &fakeKeffectsWorkflow{instanceID: 10}
	client := NewKlingWorkflow(storage, rpc).Omni()
	multiShot := false
	if _, err := client.Submit(context.Background(), KlingOmniRequest{
		Prompt: "@image_1 缓缓转身", ReferImages: []string{reference.URL},
		Resolution: "720p", Duration: "5", AspectRatio: "9:16",
		Audio: "off", MultiShot: &multiShot,
	}); err != nil {
		t.Fatal(err)
	}
	args := argumentsByName(rpc.submitInput.Arguments)
	if _, ok := args["first_frame"]; ok {
		t.Fatal("reference-image Omni must omit first_frame")
	}
	if _, ok := args["model_name"]; ok {
		t.Fatal("reference-image Omni must omit model_name")
	}
	refs := args["ref_images"].GetMediaValues().GetValue()
	if len(refs) != 1 || refs[0].GetKey() != "inputs/reference.png" {
		t.Fatalf("ref_images = %+v", refs)
	}
}

func TestKlingWorkflowOmniRejectsRequestsWithoutReferences(t *testing.T) {
	rpc := &fakeKeffectsWorkflow{instanceID: 11}
	client := NewKlingWorkflow(newWorkflowTestStorage(t), rpc).Omni()
	multiShot := false
	_, err := client.Submit(context.Background(), KlingOmniRequest{
		Prompt: "纯提示词", Resolution: "720p", Duration: "5",
		AspectRatio: "16:9", Audio: "off", MultiShot: &multiShot,
	})
	if err == nil {
		t.Fatal("Omni request without reference material must fail")
	}
	if rpc.submitInput.RequestID != "" {
		t.Fatal("invalid Omni request reached keffects")
	}
}

func argumentsByName(arguments []*pb.Argument) map[string]*pb.Argument {
	result := make(map[string]*pb.Argument, len(arguments))
	for _, argument := range arguments {
		result[argument.GetName()] = argument
	}
	return result
}
