package capworker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

type recordedGatewayCall struct {
	Route     string
	RequestID string
	Features  map[string]any
}

type scriptedGateway struct {
	responses []map[string]any
	calls     []recordedGatewayCall
}

func (g *scriptedGateway) Infer(
	_ context.Context,
	route string,
	requestID string,
	features map[string]any,
) (map[string]any, error) {
	g.calls = append(g.calls, recordedGatewayCall{
		Route: route, RequestID: requestID, Features: features,
	})
	response := g.responses[0]
	g.responses = g.responses[1:]
	return response, nil
}

func TestComposeVideoAdapterUsesGatewayFFmpegPipeline(t *testing.T) {
	preserve := false
	volume := 0.6
	gateway := &scriptedGateway{responses: []map[string]any{
		{
			"video":  "agentk:artifacts:base.mp4",
			"result": `{"probe":{"duration":8.5,"width":1920,"height":1080,"fps":30,"has_audio":true,"has_video":true}}`,
		},
		{
			"video":  "agentk:artifacts:mixed.mp4",
			"result": `{"probe":{"duration":8.5,"width":1920,"height":1080,"fps":30,"has_audio":true,"has_video":true}}`,
		},
		{
			"video":  "agentk:artifacts:final.mp4",
			"result": `{"probe":{"duration":8.5,"width":1920,"height":1080,"fps":30,"has_audio":true,"has_video":true}}`,
		},
	}}
	activity := inferActivity{
		route: "ffmpeg", adapter: "compose_video", gateway: gateway,
		artifactURIs: testArtifactURIResolver(),
	}
	output, err := activity.composeVideo(t.Context(), "agentk_test", map[string]any{
		"db": "upload", "table": "ylab-stunt",
		"videoUrls":             []string{"blob://upload-ylab-stunt/a.mp4", "https://cdn.example/b.mp4"},
		"size":                  "1080p",
		"preserveOriginalAudio": preserve,
		"audioTracks": []map[string]any{{
			"url": "blob://upload-ylab-stunt/voice.m4a", "startSeconds": 1.25,
			"trimStartSeconds": 0.5, "durationSeconds": 2.25,
			"volume": volume, "loop": true,
		}},
		"subtitleSegments": []map[string]any{{
			"startSeconds": 0.0, "endSeconds": 1.5, "text": "你好",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := output["artifacts"].([]map[string]any)
	if artifacts[0]["uri"] != "blob://agentk-artifacts/final.mp4" ||
		output["clipCount"] != 2 || output["durationSeconds"] != 8.5 {
		t.Fatalf("output = %+v", output)
	}
	if len(gateway.calls) != 3 {
		t.Fatalf("gateway calls = %+v", gateway.calls)
	}
	for _, call := range gateway.calls {
		if call.Route != "ffmpeg" {
			t.Fatalf("route = %q", call.Route)
		}
	}

	baseSteps := decodePipeline(t, gateway.calls[0].Features["pipeline"])
	if len(baseSteps) != 2 || baseSteps[0].Op != "concat_av" || baseSteps[1].Op != "probe" {
		t.Fatalf("base pipeline = %+v", baseSteps)
	}
	if baseSteps[0].Params["size"] != "1080p" || baseSteps[0].Params["fit"] != "cover" ||
		baseSteps[0].Params["fps"] != float64(30) ||
		baseSteps[0].Params["sample_rate"] != float64(48000) ||
		baseSteps[0].Params["crf"] != float64(23) {
		t.Fatalf("base params = %+v", baseSteps[0].Params)
	}
	var baseRefs struct {
		Videos []string `json:"videos"`
	}
	decodeJSONString(t, gateway.calls[0].Features["refs"], &baseRefs)
	if len(baseRefs.Videos) != 2 || baseRefs.Videos[0] != "upload:ylab-stunt:a.mp4" {
		t.Fatalf("base refs = %+v", baseRefs)
	}
	for _, call := range gateway.calls {
		assertAdapterStorageTarget(t, call.Features, "upload", "ylab-stunt")
	}

	var mixRefs struct {
		Video  string   `json:"video"`
		Audios []string `json:"audios"`
	}
	decodeJSONString(t, gateway.calls[1].Features["refs"], &mixRefs)
	if mixRefs.Video != "agentk:artifacts:base.mp4" || len(mixRefs.Audios) != 1 ||
		mixRefs.Audios[0] != "upload:ylab-stunt:voice.m4a" {
		t.Fatalf("mix refs = %+v", mixRefs)
	}
	mixSteps := decodePipeline(t, gateway.calls[1].Features["pipeline"])
	offsets := mixSteps[0].Params["offsets_ms"].([]any)
	trimStarts := mixSteps[0].Params["trim_starts"].([]any)
	durations := mixSteps[0].Params["durations"].([]any)
	loops := mixSteps[0].Params["loops"].([]any)
	if offsets[0].(float64) != 1250 || trimStarts[0].(float64) != 0.5 ||
		durations[0].(float64) != 2.25 || !loops[0].(bool) ||
		mixSteps[0].Params["video_gain"] != float64(0) ||
		mixSteps[0].Params["duration_mode"] != "first" ||
		mixSteps[0].Params["normalize"] != true || mixSteps[0].Params["limit"] != false {
		t.Fatalf("mix params = %+v", mixSteps[0].Params)
	}

	subtitleSteps := decodePipeline(t, gateway.calls[2].Features["pipeline"])
	if subtitleSteps[0].Params["crf"] != float64(23) {
		t.Fatalf("subtitle params = %+v", subtitleSteps[0].Params)
	}
	subtitle, ok := gateway.calls[2].Features["subtitle"].(string)
	if !ok || len(subtitle) < len("base64:") || subtitle[:len("base64:")] != "base64:" {
		t.Fatalf("subtitle = %#v", gateway.calls[2].Features["subtitle"])
	}
	decoded, err := base64.StdEncoding.DecodeString(subtitle[len("base64:"):])
	if err != nil || string(decoded) != "1\n00:00:00,000 --> 00:00:01,500\n你好\n\n" {
		t.Fatalf("decoded subtitle = %q, err = %v", decoded, err)
	}
}

func TestExtractAudioAdapterUsesGatewayFFmpegPipeline(t *testing.T) {
	gateway := &scriptedGateway{responses: []map[string]any{{
		"audio":  "agentk:artifacts:clip.mp3",
		"result": `{"probe":{"duration":3,"width":null,"height":null,"fps":null,"has_audio":true,"has_video":false}}`,
	}}}
	activity := inferActivity{
		route: "ffmpeg", adapter: "extract_audio", gateway: gateway,
		artifactURIs: testArtifactURIResolver(),
	}
	output, err := activity.extractAudio(t.Context(), "agentk_test", map[string]any{
		"db": "upload", "table": "ylab-stunt",
		"videoUrl":     "blob://upload-ylab-stunt/video.mp4",
		"startSeconds": 2.0, "durationSeconds": 3.0, "format": "mp3",
		"bitRate": "128k", "sampleRate": 16000, "channels": 1,
		"normalizeLoudness": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := output["artifacts"].([]map[string]any)
	if artifacts[0]["uri"] != "blob://agentk-artifacts/clip.mp3" ||
		output["durationSeconds"] != 3.0 || output["format"] != "mp3" ||
		output["sampleRate"] != 16000 || output["channels"] != 1 {
		t.Fatalf("output = %+v", output)
	}
	if len(gateway.calls) != 1 || gateway.calls[0].Route != "ffmpeg" {
		t.Fatalf("calls = %+v", gateway.calls)
	}
	steps := decodePipeline(t, gateway.calls[0].Features["pipeline"])
	if len(steps) != 2 || steps[0].Op != "extract_audio" || steps[1].Op != "probe" {
		t.Fatalf("pipeline = %+v", steps)
	}
	if steps[0].Params["start"] != 2.0 || steps[0].Params["duration"] != 3.0 ||
		steps[0].Params["format"] != "mp3" || steps[0].Params["bit_rate"] != "128k" ||
		steps[0].Params["sample_rate"] != float64(16000) ||
		steps[0].Params["channels"] != float64(1) ||
		steps[0].Params["normalize_loudness"] != true {
		t.Fatalf("extract params = %+v", steps[0].Params)
	}
	var refs map[string]string
	decodeJSONString(t, gateway.calls[0].Features["refs"], &refs)
	if refs["video"] != "upload:ylab-stunt:video.mp4" {
		t.Fatalf("refs = %+v", refs)
	}
	assertAdapterStorageTarget(t, gateway.calls[0].Features, "upload", "ylab-stunt")
}

func TestProcessVideoAdapterUsesGatewayFFmpegPipeline(t *testing.T) {
	gateway := &scriptedGateway{responses: []map[string]any{{
		"video":  "agentk:artifacts:processed.mp4",
		"result": `{"probe":{"duration":1.6,"width":1080,"height":1920,"fps":30,"has_audio":true,"has_video":true}}`,
	}}}
	activity := inferActivity{
		route: "ffmpeg", adapter: "process_video", gateway: gateway,
		artifactURIs: testArtifactURIResolver(),
	}
	output, err := activity.processVideo(t.Context(), "agentk_test", map[string]any{
		"db": "upload", "table": "ylab-stunt",
		"videoUrl": "blob://upload-ylab-stunt/source.mp4",
		"operations": []map[string]any{
			{"type": "trim", "startSeconds": 0.0, "durationSeconds": 2.0},
			{"type": "reframe", "width": 1081, "height": 1921},
			{"type": "denoise"},
			{"type": "normalize"},
			{"type": "speed", "factor": 1.25},
		},
		"quality": "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := output["artifacts"].([]map[string]any)
	params := artifacts[0]["params"].(map[string]any)
	if artifacts[0]["uri"] != "blob://agentk-artifacts/processed.mp4" ||
		output["durationSeconds"] != 1.6 || output["width"] != 1080 ||
		output["height"] != 1920 || params["quality"] != "high" ||
		params["videoCodec"] != "h264" || params["audioCodec"] != "aac" {
		t.Fatalf("output = %+v", output)
	}
	if len(gateway.calls) != 1 || gateway.calls[0].Route != "ffmpeg" ||
		gateway.calls[0].RequestID != "agentk_test-process-video" {
		t.Fatalf("calls = %+v", gateway.calls)
	}
	steps := decodePipeline(t, gateway.calls[0].Features["pipeline"])
	if len(steps) != 2 || steps[0].Op != "process_video" || steps[1].Op != "probe" ||
		steps[0].Params["quality"] != "high" {
		t.Fatalf("pipeline = %+v", steps)
	}
	operations, ok := steps[0].Params["operations"].([]any)
	if !ok || len(operations) != 5 {
		t.Fatalf("operations = %#v", steps[0].Params["operations"])
	}
	reframe := operations[1].(map[string]any)
	denoise := operations[2].(map[string]any)
	if reframe["fit"] != "cover" || denoise["strength"] != "medium" {
		t.Fatalf("normalized operations = %+v", operations)
	}
	var refs map[string]string
	decodeJSONString(t, gateway.calls[0].Features["refs"], &refs)
	if refs["video"] != "upload:ylab-stunt:source.mp4" {
		t.Fatalf("refs = %+v", refs)
	}
	assertAdapterStorageTarget(t, gateway.calls[0].Features, "upload", "ylab-stunt")
}

func TestProcessVideoAdapterRejectsInvalidOperations(t *testing.T) {
	activity := inferActivity{
		route: "ffmpeg", adapter: "process_video", gateway: &scriptedGateway{},
	}
	for _, operations := range []any{
		[]map[string]any{},
		[]map[string]any{{"type": "trim", "durationSeconds": 0}},
		[]map[string]any{{"type": "speed", "factor": 3}},
		[]map[string]any{{"type": "reframe", "width": 1, "height": 1080}},
		[]map[string]any{{"type": "raw_ffmpeg"}},
	} {
		_, err := activity.processVideo(t.Context(), "agentk_test", map[string]any{
			"videoUrl": "https://cdn.example/video.mp4", "operations": operations,
		})
		callError, ok := err.(*CallError)
		if !ok || !callError.Permanent || callError.Code != "INVALID_ARGUMENT" {
			t.Fatalf("operations %#v error = %#v", operations, err)
		}
	}
}

func testArtifactURIResolver() artifactURIResolver {
	return newArtifactURIResolver()
}

func TestExtractAudioAdapterRejectsUnsupportedFormat(t *testing.T) {
	activity := inferActivity{
		route: "ffmpeg", adapter: "extract_audio", gateway: &scriptedGateway{},
	}
	_, err := activity.extractAudio(t.Context(), "agentk_test", map[string]any{
		"videoUrl": "https://cdn.example/video.mp4", "format": "flac",
	})
	callError, ok := err.(*CallError)
	if !ok || !callError.Permanent || callError.Code != "INVALID_ARGUMENT" {
		t.Fatalf("error = %#v", err)
	}
}

func TestComposeVideoAdapterStripsNormalizedTimelineWithoutExternalTracks(t *testing.T) {
	preserve := false
	gateway := &scriptedGateway{responses: []map[string]any{{
		"video":  "upload:ylab-stunt:silent.mp4",
		"result": `{"probe":{"duration":2,"width":640,"height":480,"fps":30,"has_audio":false,"has_video":true}}`,
	}}}
	activity := inferActivity{
		route: "ffmpeg", adapter: "compose_video", gateway: gateway,
		artifactURIs: testArtifactURIResolver(),
	}
	_, err := activity.composeVideo(t.Context(), "agentk_test", map[string]any{
		"videoUrls":             []string{"blob://upload-ylab-stunt/source.mp4"},
		"preserveOriginalAudio": preserve,
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := decodePipeline(t, gateway.calls[0].Features["pipeline"])
	if len(steps) != 3 || steps[0].Op != "concat_av" ||
		steps[1].Op != "strip_audio" || steps[2].Op != "probe" {
		t.Fatalf("pipeline = %+v", steps)
	}
}

func TestNormalizeFFmpegRef(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"blob://upload-ylab-stunt/path/video.mp4", "upload:ylab-stunt:path/video.mp4"},
		{"agentk:artifacts:path/video.mp4", "agentk:artifacts:path/video.mp4"},
		{"https://cdn.example/path/video.mp4?token=x", "https://cdn.example/path/video.mp4?token=x"},
	}
	for _, test := range tests {
		got, err := normalizeFFmpegRef(test.input)
		if err != nil || got != test.want {
			t.Fatalf("normalizeFFmpegRef(%q) = %q, %v", test.input, got, err)
		}
	}
	for _, input := range []string{
		"blob://noseparator/key.mp4",
		"blob://upload-ylab-stunt/../escape.mp4",
		"file:///tmp/video.mp4",
	} {
		if _, err := normalizeFFmpegRef(input); err == nil {
			t.Fatalf("normalizeFFmpegRef(%q) unexpectedly succeeded", input)
		}
	}
}

func decodePipeline(t *testing.T, raw any) []struct {
	Op     string         `json:"op"`
	Params map[string]any `json:"params"`
} {
	t.Helper()
	var steps []struct {
		Op     string         `json:"op"`
		Params map[string]any `json:"params"`
	}
	decodeJSONString(t, raw, &steps)
	return steps
}

func decodeJSONString(t *testing.T, raw any, target any) {
	t.Helper()
	encoded, ok := raw.(string)
	if !ok {
		t.Fatalf("JSON string = %#v", raw)
	}
	if err := json.Unmarshal([]byte(encoded), target); err != nil {
		t.Fatal(err)
	}
}

func assertAdapterStorageTarget(t *testing.T, features map[string]any, db, table string) {
	t.Helper()
	if features["db"] != db || features["table"] != table {
		t.Fatalf("features = %+v", features)
	}
}

func TestFFmpegAdaptersAllowGatewayDefaultOutputTarget(t *testing.T) {
	target, err := validateFFmpegOutputTarget("", "")
	if err != nil || target != (ffmpegOutputTarget{}) {
		t.Fatalf("target = %+v, err = %v", target, err)
	}
}

func TestFFmpegAdaptersRejectPartialOutputTarget(t *testing.T) {
	for _, values := range [][2]string{{"upload", ""}, {"", "ylab-stunt"}} {
		if _, err := validateFFmpegOutputTarget(values[0], values[1]); err == nil {
			t.Fatalf("target %q/%q accepted", values[0], values[1])
		}
	}
}
