package keffects

import (
	"errors"
	"strings"
	"testing"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

func media(key string) *pb.Media { return &pb.Media{Bucket: "work", Key: key} }

func argumentsByName(arguments []*pb.Argument) map[string]*pb.Argument {
	result := make(map[string]*pb.Argument, len(arguments))
	for _, argument := range arguments {
		result[argument.GetName()] = argument
	}
	return result
}

func mustSubmit(t *testing.T, input interface{ SubmitInput() (SubmitInput, error) }) SubmitInput {
	t.Helper()
	submit, err := input.SubmitInput()
	if err != nil {
		t.Fatal(err)
	}
	return submit
}

func mustReject(t *testing.T, name string, input interface{ SubmitInput() (SubmitInput, error) }) {
	t.Helper()
	_, err := input.SubmitInput()
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("%s: error = %v, want ErrInvalidInput", name, err)
	}
}

func TestOutputMissingIsSentinel(t *testing.T) {
	_, err := FindMedia(nil, "image")
	if !errors.Is(err, ErrOutputMissing) {
		t.Fatalf("error = %v, want ErrOutputMissing", err)
	}
	_, err = FindMedia([]*pb.Argument{StringArgument("image", "x")}, "image")
	if errors.Is(err, ErrOutputMissing) {
		t.Fatal("wrong-typed output must not report ErrOutputMissing")
	}
}

func TestKlingVideoTextOnly(t *testing.T) {
	submit := mustSubmit(t, KlingVideoInput{
		Prompt: "海浪拍打礁石", Duration: 5, Resolution: "720p", AspectRatio: "16:9",
		EnableAudio: true, EnableMultiShot: true,
	})
	if submit.WorkflowName != KlingVideoWorkflowName || submit.RequestID != "" {
		t.Fatalf("submit = %+v", submit)
	}
	args := argumentsByName(submit.Arguments)
	if args["duration"].GetFloatValue() != 5 || args["resolution"].GetStringValue() != "720p" ||
		args["aspect_ratio"].GetStringValue() != "16:9" ||
		!args["enable_audio"].GetBoolValue() || !args["enable_multishot"].GetBoolValue() {
		t.Fatalf("arguments = %+v", args)
	}
	for _, name := range []string{"first_frame", "tail_image", "ref_images", "ref_videos", "subject_lists", "model_name"} {
		if _, ok := args[name]; ok {
			t.Fatalf("text-only submission must omit %s", name)
		}
	}
}

func TestKlingVideoFirstFrameDefaultsAspectRatioToAuto(t *testing.T) {
	submit := mustSubmit(t, KlingVideoInput{
		Prompt: "转身", Duration: 5, Resolution: "1080p",
		FirstFrame: media("first.png"), TailImage: media("last.png"),
	})
	args := argumentsByName(submit.Arguments)
	if args["aspect_ratio"].GetStringValue() != "auto" {
		t.Fatalf("aspect_ratio = %q", args["aspect_ratio"].GetStringValue())
	}
	if args["first_frame"].GetMediaValue().GetKey() != "first.png" ||
		args["tail_image"].GetMediaValue().GetKey() != "last.png" {
		t.Fatalf("frames = %+v", args)
	}
	if _, ok := args["model_name"]; ok {
		t.Fatal("first-frame submission must omit model_name")
	}
}

func TestKlingVideoReferenceVideoSelectsModel(t *testing.T) {
	for resolution, want := range map[string]string{
		"1080p": "Kling-v3_0_1-aio-omni-1080p-prod",
		"720p":  "Kling-v3_0_1-aio-omni-720p-prod",
	} {
		submit := mustSubmit(t, KlingVideoInput{
			Prompt: "参考运镜", Duration: 5, Resolution: resolution,
			RefVideos: []*pb.Media{media("ref.mp4")},
		})
		args := argumentsByName(submit.Arguments)
		if args["model_name"].GetStringValue() != want {
			t.Fatalf("%s model_name = %q", resolution, args["model_name"].GetStringValue())
		}
		if args["aspect_ratio"].GetStringValue() != "auto" {
			t.Fatalf("aspect_ratio = %q", args["aspect_ratio"].GetStringValue())
		}
	}
}

func TestKlingVideoReferenceImagesAndSubjects(t *testing.T) {
	submit := mustSubmit(t, KlingVideoInput{
		Prompt: "@image_1 挥手", Duration: 5, Resolution: "720p", AspectRatio: "9:16",
		RefImages: []*pb.Media{media("a.png"), media("b.png")}, SubjectLists: []string{"subject-1"},
	})
	args := argumentsByName(submit.Arguments)
	if len(args["ref_images"].GetMediaValues().GetValue()) != 2 ||
		args["subject_lists"].GetStringValues().GetValue()[0] != "subject-1" {
		t.Fatalf("arguments = %+v", args)
	}
	if _, ok := args["model_name"]; ok {
		t.Fatal("reference-image submission must omit model_name")
	}
}

func TestKlingVideoRejectsContractViolations(t *testing.T) {
	valid := KlingVideoInput{Prompt: "p", Duration: 5, Resolution: "720p", AspectRatio: "16:9"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	// Scalar violations are caught by Validate before any media is staged.
	for name, mutate := range map[string]func(*KlingVideoInput){
		"empty prompt":  func(in *KlingVideoInput) { in.Prompt = " " },
		"zero duration": func(in *KlingVideoInput) { in.Duration = 0 },
		"4k resolution": func(in *KlingVideoInput) { in.Resolution = "4k" },
		"two ref videos": func(in *KlingVideoInput) {
			in.RefVideos = make([]*pb.Media, KlingVideoMaxRefVideos+1)
		},
	} {
		input := valid
		mutate(&input)
		if !errors.Is(input.Validate(), ErrInvalidInput) {
			t.Fatalf("%s: Validate() accepted invalid input", name)
		}
		mustReject(t, name, input)
	}
	// Media violations are only visible once media is attached.
	for name, mutate := range map[string]func(*KlingVideoInput){
		"tail without first": func(in *KlingVideoInput) { in.TailImage = media("last.png") },
		"unstaged media":     func(in *KlingVideoInput) { in.FirstFrame = &pb.Media{Bucket: "work"} },
	} {
		input := valid
		mutate(&input)
		if err := input.Validate(); err != nil {
			t.Fatalf("%s: Validate() = %v, want nil", name, err)
		}
		mustReject(t, name, input)
	}
}

func TestParseKlingVideo(t *testing.T) {
	video, err := ParseKlingVideo(&pb.QueryResponse{Outputs: []*pb.Argument{
		MediaArgument("res_video", "final", "out.mp4"),
	}})
	if err != nil || video.GetKey() != "out.mp4" {
		t.Fatalf("video = %+v, %v", video, err)
	}
	if _, err := ParseKlingVideo(&pb.QueryResponse{}); !errors.Is(err, ErrOutputMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestKColorTextToImage(t *testing.T) {
	submit := mustSubmit(t, KColorTextToImageInput{
		Prompt: " 雨夜街道 ", AspectRatio: "16:9", Resolution: "2k",
		Style: "cinematic", PromptJSON: `{"lighting":"neon"}`,
	})
	if submit.WorkflowName != KColorTextToImageWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	for name, want := range map[string]string{
		"aspect_ratio": "16:9", "resolution": "2k", "prompt": "雨夜街道",
		"style": "cinematic", "prompt_json": `{"lighting":"neon"}`,
	} {
		if got, err := FindString(submit.Arguments, name); err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	args := argumentsByName(mustSubmit(t, KColorTextToImageInput{
		Prompt: "图", AspectRatio: "1:1", Resolution: "1k",
	}).Arguments)
	if _, ok := args["style"]; ok {
		t.Fatal("empty style must be omitted")
	}
	if _, ok := args["prompt_json"]; ok {
		t.Fatal("empty prompt_json must be omitted")
	}
}

func TestKColorReferenceImage(t *testing.T) {
	submit := mustSubmit(t, KColorReferenceImageInput{
		Prompt: "换背景", AspectRatio: "3:4", Resolution: "1k",
		ReferenceImages: []*pb.Media{media("person.png"), media("clothes.png")},
	})
	if submit.WorkflowName != KColorReferenceImageWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	refs, err := FindMediaValues(submit.Arguments, "ref_image_list")
	if err != nil || len(refs) != 2 || refs[1].GetKey() != "clothes.png" {
		t.Fatalf("ref_image_list = %+v, %v", refs, err)
	}
}

func TestKColorRejectsContractViolations(t *testing.T) {
	validT2I := KColorTextToImageInput{Prompt: "图", AspectRatio: "1:1", Resolution: "1k"}
	for name, mutate := range map[string]func(*KColorTextToImageInput){
		"missing aspect ratio": func(in *KColorTextToImageInput) { in.AspectRatio = "" },
		"bad aspect ratio":     func(in *KColorTextToImageInput) { in.AspectRatio = "2:1" },
		"missing resolution":   func(in *KColorTextToImageInput) { in.Resolution = "" },
		"bad resolution":       func(in *KColorTextToImageInput) { in.Resolution = "1K" },
		"bad prompt JSON":      func(in *KColorTextToImageInput) { in.PromptJSON = "{" },
		"long prompt":          func(in *KColorTextToImageInput) { in.Prompt = strings.Repeat("图", 1001) },
	} {
		input := validT2I
		mutate(&input)
		if !errors.Is(input.Validate(), ErrInvalidInput) {
			t.Fatalf("t2i %s: Validate() accepted invalid input", name)
		}
		mustReject(t, "t2i "+name, input)
	}

	// The scalar contract can be validated before reference images are staged.
	unstaged := KColorReferenceImageInput{Prompt: "编辑", AspectRatio: "3:4", Resolution: "2k"}
	if err := unstaged.Validate(); err != nil {
		t.Fatalf("Validate() without media = %v", err)
	}
	for name, mutate := range map[string]func(*KColorReferenceImageInput){
		"bad resolution": func(in *KColorReferenceImageInput) { in.Resolution = "2K" },
		"long prompt":    func(in *KColorReferenceImageInput) { in.Prompt = strings.Repeat("图", 1001) },
	} {
		input := unstaged
		mutate(&input)
		if !errors.Is(input.Validate(), ErrInvalidInput) {
			t.Fatalf("edit %s: Validate() accepted invalid input", name)
		}
	}
	mustReject(t, "edit no references", unstaged)
	tooMany := unstaged
	tooMany.ReferenceImages = make([]*pb.Media, KColorMaxReferenceImages+1)
	mustReject(t, "edit too many references", tooMany)
}

func TestCustomVoices(t *testing.T) {
	submit := mustSubmit(t, CustomVoicesInput{Audio: media("voice.wav")})
	if submit.WorkflowName != CustomVoicesWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	if audio, err := FindMedia(submit.Arguments, "audio"); err != nil || audio.GetKey() != "voice.wav" {
		t.Fatalf("audio = %+v, %v", audio, err)
	}
	mustReject(t, "missing audio", CustomVoicesInput{})

	voices, err := ParseCustomVoices(&pb.QueryResponse{Outputs: []*pb.Argument{
		MediaValuesArgument("voice_embedding_list", []*pb.Media{media("voice.bin")}),
	}})
	if err != nil || len(voices) != 1 {
		t.Fatalf("voices = %+v, %v", voices, err)
	}
}

func TestCustomElementsImageMode(t *testing.T) {
	submit := mustSubmit(t, CustomElementsInput{
		Name: "host", Description: "demo",
		FrontalImage: media("a.png"), ReferImages: []*pb.Media{media("b.png")},
		VoiceEmbeddings: []*pb.Media{media("voice.bin")},
	})
	if submit.WorkflowName != CustomElementsWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	args := argumentsByName(submit.Arguments)
	if args["reference_type"].GetStringValue() != "IMAGE" ||
		args["element_name"].GetStringValue() != "host" ||
		args["frontal_image"].GetMediaValue().GetKey() != "a.png" ||
		args["refer_images"].GetMediaValues().GetValue()[0].GetKey() != "b.png" ||
		len(args["voice_embeddings"].GetMediaValues().GetValue()) != 1 {
		t.Fatalf("arguments = %+v", args)
	}
	if _, ok := args["video"]; ok {
		t.Fatal("image mode must omit video")
	}

	single := argumentsByName(mustSubmit(t, CustomElementsInput{
		Name: "solo", FrontalImage: media("only.png"),
	}).Arguments)
	if refs := single["refer_images"].GetMediaValues().GetValue(); len(refs) != 1 || refs[0].GetKey() != "only.png" {
		t.Fatalf("single-image refer_images = %+v", refs)
	}
}

func TestCustomElementsVideoMode(t *testing.T) {
	args := argumentsByName(mustSubmit(t, CustomElementsInput{
		Name: "host", FrontalImage: media("frame.jpg"), Video: media("clip.mp4"),
	}).Arguments)
	if args["reference_type"].GetStringValue() != "VIDEO" ||
		args["video"].GetMediaValue().GetKey() != "clip.mp4" ||
		args["frontal_image"].GetMediaValue().GetKey() != "frame.jpg" {
		t.Fatalf("arguments = %+v", args)
	}
	if _, ok := args["refer_images"]; ok {
		t.Fatal("video mode must omit refer_images")
	}
	mustReject(t, "video with refer images", CustomElementsInput{
		Name: "host", FrontalImage: media("frame.jpg"), Video: media("clip.mp4"),
		ReferImages: []*pb.Media{media("b.png")},
	})
	mustReject(t, "video without frontal image", CustomElementsInput{
		Name: "host", Video: media("clip.mp4"),
	})
	mustReject(t, "missing name", CustomElementsInput{FrontalImage: media("a.png")})
	if !errors.Is((CustomElementsInput{}).Validate(), ErrInvalidInput) {
		t.Fatal("Validate() accepted an empty name")
	}
	if err := (CustomElementsInput{Name: "host"}).Validate(); err != nil {
		t.Fatalf("Validate() without media = %v", err)
	}
}

func TestParseCustomElements(t *testing.T) {
	output, err := ParseCustomElements(&pb.QueryResponse{Outputs: []*pb.Argument{
		StringArgument("subject_list", "subject"),
		StringValuesArgument("subject_lists", []string{"subject-a"}),
		MediaValuesArgument("refer_images", []*pb.Media{media("a.png")}),
	}})
	if err != nil || output.SubjectList != "subject" || len(output.SubjectLists) != 1 ||
		len(output.ReferImages) != 1 || output.ReferVideos != nil || output.ReferVoices != nil {
		t.Fatalf("output = %+v, %v", output, err)
	}
	if _, err := ParseCustomElements(&pb.QueryResponse{Outputs: []*pb.Argument{
		StringArgument("subject_list", "subject"),
	}}); !errors.Is(err, ErrOutputMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestIdentifyFace(t *testing.T) {
	submit := mustSubmit(t, IdentifyFaceInput{Video: media("clip.mp4")})
	if submit.WorkflowName != IdentifyFaceWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	mustReject(t, "missing video", IdentifyFaceInput{})

	faces, err := ParseIdentifyFace(&pb.QueryResponse{Outputs: []*pb.Argument{
		StringArgument("first_face_id", "face-1"),
		StringArgument("face_index_key", "index-key"),
		StringValuesArgument("face_id_list", []string{"face-1"}),
		MediaValuesArgument("face_image_list", []*pb.Media{media("face.jpg")}),
	}})
	if err != nil || faces.FirstFaceID != "face-1" || faces.FaceIndexKey != "index-key" ||
		len(faces.FaceIDs) != 1 || len(faces.FaceImages) != 1 {
		t.Fatalf("faces = %+v, %v", faces, err)
	}
	if _, err := ParseIdentifyFace(&pb.QueryResponse{Outputs: []*pb.Argument{
		StringArgument("first_face_id", "face-1"),
		StringArgument("face_index_key", "index-key"),
		StringValuesArgument("face_id_list", []string{"face-1", "face-2"}),
		MediaValuesArgument("face_image_list", []*pb.Media{media("face.jpg")}),
	}}); err == nil {
		t.Fatal("mismatched face lists were accepted")
	}
}

func TestLipSync(t *testing.T) {
	submit := mustSubmit(t, LipSyncInput{
		Video: media("clip.mp4"), Audio: media("speech.wav"),
		FaceIndexKey: "index-key", FaceID: "face-1",
		SoundInsertTime: 100, SoundStartTime: 0, SoundEndTime: 1000,
		SoundVolume: 1, OriginalAudioVolume: 0.5,
	})
	if submit.WorkflowName != LipSyncWorkflowName {
		t.Fatalf("workflow = %q", submit.WorkflowName)
	}
	args := argumentsByName(submit.Arguments)
	if args["face_id_key"].GetStringValue() != "index-key" ||
		args["face_id"].GetStringValue() != "face-1" ||
		args["sound_insert_time"].GetInt64Value() != 100 ||
		args["sound_end_time"].GetInt64Value() != 1000 ||
		args["sound_volume"].GetFloatValue() != 1 ||
		args["original_audio_volume"].GetFloatValue() != 0.5 {
		t.Fatalf("arguments = %+v", args)
	}

	unstaged := LipSyncInput{FaceIndexKey: "index-key", FaceID: "face-1", SoundEndTime: 1000}
	if err := unstaged.Validate(); err != nil {
		t.Fatalf("Validate() without media = %v", err)
	}
	for name, mutate := range map[string]func(*LipSyncInput){
		"missing face id":  func(in *LipSyncInput) { in.FaceID = "" },
		"end before start": func(in *LipSyncInput) { in.SoundStartTime = 1000 },
		"negative volume":  func(in *LipSyncInput) { in.SoundVolume = -1 },
	} {
		input := unstaged
		mutate(&input)
		if !errors.Is(input.Validate(), ErrInvalidInput) {
			t.Fatalf("%s: Validate() accepted invalid input", name)
		}
	}
	mustReject(t, "missing media", unstaged)
	missingAudio := unstaged
	missingAudio.Video = media("clip.mp4")
	mustReject(t, "missing audio", missingAudio)

	video, err := ParseLipSync(&pb.QueryResponse{Outputs: []*pb.Argument{
		MediaArgument("video", "final", "lip.mp4"),
	}})
	if err != nil || video.GetKey() != "lip.mp4" {
		t.Fatalf("video = %+v, %v", video, err)
	}
}
