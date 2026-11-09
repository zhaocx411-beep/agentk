package keffects

import (
	"testing"

	pb "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gen/keffects/v1"
)

func TestArgumentHelpers(t *testing.T) {
	if got := StringArgument("prompt", "hello").GetStringValue(); got != "hello" {
		t.Fatalf("string value = %q, want hello", got)
	}
	if got := BoolArgument("audio", true).GetBoolValue(); !got {
		t.Fatal("bool value = false, want true")
	}
	if got := FloatArgument("duration", 5).GetFloatValue(); got != 5 {
		t.Fatalf("float value = %v, want 5", got)
	}
	if got := Int64Argument("sound_end_time", 3000).GetInt64Value(); got != 3000 {
		t.Fatalf("int64 value = %v, want 3000", got)
	}
	if got := StringValuesArgument("subject_lists", []string{"a", "b"}).
		GetStringValues().GetValue(); len(got) != 2 {
		t.Fatalf("string values length = %d, want 2", len(got))
	}
	if got := MediaValuesArgument("refs", []*pb.Media{{Bucket: "b", Key: "k"}}).
		GetMediaValues().GetValue(); len(got) != 1 {
		t.Fatalf("media values length = %d, want 1", len(got))
	}
}

func TestFindScalarAndArrayOutputs(t *testing.T) {
	arguments := []*pb.Argument{
		StringArgument("subject_list", "subject"),
		StringValuesArgument("subject_lists", []string{"subject-a", "subject-b"}),
		MediaValuesArgument("faces", []*pb.Media{{Bucket: "final", Key: "face.jpg"}}),
	}
	if got, err := FindString(arguments, "subject_list"); err != nil || got != "subject" {
		t.Fatalf("FindString() = %q, %v", got, err)
	}
	if got, err := FindStringValues(arguments, "subject_lists"); err != nil || len(got) != 2 {
		t.Fatalf("FindStringValues() = %+v, %v", got, err)
	}
	if got, err := FindMediaValues(arguments, "faces"); err != nil || len(got) != 1 {
		t.Fatalf("FindMediaValues() = %+v, %v", got, err)
	}
}

func TestFindMedia(t *testing.T) {
	media, err := FindMedia([]*pb.Argument{
		MediaArgument("result", "bucket", "key"),
	}, "result")
	if err != nil {
		t.Fatalf("FindMedia() error = %v", err)
	}
	if media.GetBucket() != "bucket" || media.GetKey() != "key" {
		t.Fatalf("FindMedia() = %v, want bucket/key", media)
	}

	if _, err := FindMedia(nil, "missing"); err == nil {
		t.Fatal("FindMedia() missing error = nil")
	}
}
