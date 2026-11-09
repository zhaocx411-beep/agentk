package keffects

import (
	"errors"
	"strings"
	"testing"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
	"google.golang.org/protobuf/proto"
)

func TestKlingV4Bindings(t *testing.T) {
	prompt := "  Image 1 与 @hero 说话，用 Audio 1 的声音\n"
	subject := " [ {\"name\": \"hero\", \"audios\": [\"upload:work:voice.wav\"]} ] "
	first, tail := media("first.png"), media("tail.png")
	images := []*pb.Media{media("b.png"), media("a.png")}
	base := []*pb.Media{media("base.mp4")}
	videos := []*pb.Media{media("second.mp4"), media("first.mp4")}
	middle := []*pb.Media{media("middle2.png"), media("middle1.png")}
	voices := []*pb.Media{media("voice.wav"), media("voice.mp3")}
	prePE := false
	common := []*pb.Argument{
		StringArgument("prompt", prompt),
		FloatArgument("duration", 5.04),
		StringArgument("aspect_ratio", "37:30"),
		BoolArgument("enable_intent", true),
		MediaArgument("first_frame", "work", "first.png"),
		MediaArgument("tail_frame", "work", "tail.png"),
		MediaValuesArgument("refer_images", images),
		MediaValuesArgument("base_videos", base),
		MediaValuesArgument("refer_videos", videos),
		StringArgument("subject_list", subject),
	}
	for _, tc := range []struct {
		name     string
		input    interface{ SubmitInput() (SubmitInput, error) }
		workflow string
		extra    []*pb.Argument
	}{
		{
			name: "v4", workflow: "kling_v4",
			input: KlingV4Input{
				Prompt: prompt, Duration: 5.04, AspectRatio: "37:30", EnableIntent: true,
				FirstFrame: first, TailFrame: tail, ReferImages: images,
				BaseVideos: base, ReferVideos: videos, SubjectList: subject,
				Resolution: "720p", MiddleFrames: middle, ReferVoices: voices, PrePE: &prePE,
			},
			extra: []*pb.Argument{
				StringArgument("resolution", "720p"),
				MediaValuesArgument("middle_frames", middle),
				MediaValuesArgument("refer_voices", voices),
				BoolArgument("pre_pe", false),
			},
		},
		{
			name: "flash", workflow: "kling_v4_flash",
			input: KlingV4FlashInput{
				Prompt: prompt, Duration: 5.04, AspectRatio: "37:30", EnableIntent: true,
				FirstFrame: first, TailFrame: tail, ReferImages: images,
				BaseVideos: base, ReferVideos: videos, SubjectList: subject,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			submit := mustSubmit(t, tc.input)
			if submit.WorkflowName != tc.workflow || submit.WorkflowID != 0 || submit.RequestID != "" {
				t.Fatalf("submit = %+v", submit)
			}
			want := append(append([]*pb.Argument{}, common...), tc.extra...)
			assertKlingV4Arguments(t, submit, want)
		})
	}
}

func assertKlingV4Arguments(t *testing.T, submit SubmitInput, want []*pb.Argument) {
	t.Helper()
	got := argumentsByName(submit.Arguments)
	if len(submit.Arguments) != len(want) || len(got) != len(want) {
		t.Fatalf("arguments = %v, want exactly %v", submit.Arguments, want)
	}
	for _, argument := range want {
		if !proto.Equal(got[argument.GetName()], argument) {
			t.Errorf("%s = %v, want %v", argument.GetName(), got[argument.GetName()], argument)
		}
	}
}

func TestKlingV4Defaults(t *testing.T) {
	for name, input := range map[string]interface{ SubmitInput() (SubmitInput, error) }{
		"v4": KlingV4Input{
			Prompt: "cat", ReferImages: []*pb.Media{}, MiddleFrames: []*pb.Media{},
			BaseVideos: []*pb.Media{}, ReferVideos: []*pb.Media{}, ReferVoices: []*pb.Media{},
		},
		"flash": KlingV4FlashInput{Prompt: "cat", ReferImages: []*pb.Media{}},
	} {
		t.Run(name, func(t *testing.T) {
			assertKlingV4Arguments(t, mustSubmit(t, input), []*pb.Argument{
				StringArgument("prompt", "cat"),
				BoolArgument("enable_intent", false),
			})
		})
	}
}

func TestKlingV4PrePE(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		submit := mustSubmit(t, KlingV4Input{PrePE: &enabled})
		assertKlingV4Arguments(t, submit, []*pb.Argument{
			BoolArgument("enable_intent", false),
			BoolArgument("pre_pe", enabled),
		})
	}
}

func TestKlingV4StandaloneFrames(t *testing.T) {
	for name, tc := range map[string]struct {
		input interface{ SubmitInput() (SubmitInput, error) }
		want  *pb.Argument
	}{
		"v4 tail": {
			KlingV4Input{TailFrame: media("tail.png")},
			MediaArgument("tail_frame", "work", "tail.png"),
		},
		"flash tail": {
			KlingV4FlashInput{TailFrame: media("tail.png")},
			MediaArgument("tail_frame", "work", "tail.png"),
		},
		"middle": {
			KlingV4Input{MiddleFrames: []*pb.Media{media("middle.png")}},
			MediaValuesArgument("middle_frames", []*pb.Media{media("middle.png")}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertKlingV4Arguments(t, mustSubmit(t, tc.input), []*pb.Argument{
				BoolArgument("enable_intent", false), tc.want,
			})
		})
	}
}

func TestKlingV4LeavesBusinessValidationToUpstream(t *testing.T) {
	images := make([]*pb.Media, 16)
	for i := range images {
		images[i] = media("ref.png")
	}
	prompt := strings.Repeat("文", 26001)
	subject := " opaque subject, not JSON "
	for name, input := range map[string]interface{ SubmitInput() (SubmitInput, error) }{
		"v4": KlingV4Input{
			Prompt: prompt, Duration: 31, AspectRatio: "unrecognized",
			SubjectList: subject, ReferImages: images,
		},
		"flash": KlingV4FlashInput{
			Prompt: prompt, Duration: 31, AspectRatio: "unrecognized",
			SubjectList: subject, ReferImages: images,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assertKlingV4Arguments(t, mustSubmit(t, input), []*pb.Argument{
				BoolArgument("enable_intent", false),
				StringArgument("prompt", prompt),
				FloatArgument("duration", 31),
				StringArgument("aspect_ratio", "unrecognized"),
				StringArgument("subject_list", subject),
				MediaValuesArgument("refer_images", images),
			})
		})
	}
	for _, input := range []interface{ SubmitInput() (SubmitInput, error) }{
		KlingV4Input{Duration: -1},
		KlingV4FlashInput{Duration: -1},
	} {
		assertKlingV4Arguments(t, mustSubmit(t, input), []*pb.Argument{
			BoolArgument("enable_intent", false), FloatArgument("duration", -1),
		})
	}
}

func TestKlingV4RequiresStagedMedia(t *testing.T) {
	for name, input := range map[string]interface{ SubmitInput() (SubmitInput, error) }{
		"first frame":      KlingV4Input{FirstFrame: &pb.Media{Key: "first.png"}},
		"tail frame":       KlingV4Input{TailFrame: &pb.Media{Bucket: "work"}},
		"middle frames":    KlingV4Input{MiddleFrames: []*pb.Media{nil}},
		"reference images": KlingV4Input{ReferImages: []*pb.Media{media("ok.png"), {Bucket: "work"}}},
		"base videos":      KlingV4Input{BaseVideos: []*pb.Media{{Key: "base.mp4"}}},
		"reference videos": KlingV4Input{ReferVideos: []*pb.Media{nil}},
		"voices":           KlingV4Input{ReferVoices: []*pb.Media{{}}},
		"flash":            KlingV4FlashInput{TailFrame: &pb.Media{Bucket: "work"}},
	} {
		t.Run(name, func(t *testing.T) {
			mustReject(t, name, input)
		})
	}
}

func TestParseKlingV4(t *testing.T) {
	response := &pb.QueryResponse{Outputs: []*pb.Argument{
		MediaArgument("video", "final", "video.mp4"),
		MediaArgument("wm_video", "final", "wm.mp4"),
		MediaArgument("cover", "final", "cover.jpg"),
		MediaArgument("wm_cover", "final", "wm.jpg"),
	}}
	got, err := ParseKlingV4(response)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]*pb.Media{
		"video":    {got.Video, {Bucket: "final", Key: "video.mp4"}},
		"wm_video": {got.WatermarkedVideo, {Bucket: "final", Key: "wm.mp4"}},
		"cover":    {got.Cover, {Bucket: "final", Key: "cover.jpg"}},
		"wm_cover": {got.WatermarkedCover, {Bucket: "final", Key: "wm.jpg"}},
	} {
		if !proto.Equal(pair[0], pair[1]) {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}

	got, err = ParseKlingV4(&pb.QueryResponse{Outputs: response.Outputs[:1]})
	if err != nil || got.Video == nil || got.WatermarkedVideo != nil || got.Cover != nil || got.WatermarkedCover != nil {
		t.Fatalf("video-only output = %+v, %v", got, err)
	}
}

func TestParseKlingV4MissingVideo(t *testing.T) {
	for _, response := range []*pb.QueryResponse{
		nil,
		{},
		{Outputs: []*pb.Argument{MediaArgument("res_video", "work", "old.mp4")}},
		{Outputs: []*pb.Argument{MediaArgument("wm_video", "work", "wm.mp4")}},
	} {
		if _, err := ParseKlingV4(response); !errors.Is(err, ErrOutputMissing) {
			t.Fatalf("error = %v, want ErrOutputMissing", err)
		}
	}
}

func TestParseKlingV4MalformedMedia(t *testing.T) {
	for _, name := range []string{"video", "wm_video", "cover", "wm_cover"} {
		for _, invalid := range []*pb.Argument{
			StringArgument(name, "not media"),
			{Name: name, Value: &pb.Argument_MediaValue{}},
			MediaArgument(name, "", "out"),
			MediaArgument(name, "work", ""),
		} {
			response := &pb.QueryResponse{Outputs: []*pb.Argument{invalid}}
			if name != "video" {
				response.Outputs = append(response.Outputs, MediaArgument("video", "work", "out.mp4"))
			}
			if _, err := ParseKlingV4(response); err == nil || errors.Is(err, ErrOutputMissing) {
				t.Fatalf("%s malformed output error = %v", name, err)
			}
		}
	}
}
