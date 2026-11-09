package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// execListArtifacts 执行并解码 list_artifacts 结果。
func execListArtifacts(t *testing.T, tc Context, input string) (artifacts []ArtifactInfo, note string) {
	t.Helper()
	out, err := NewListArtifactsTool().Run(context.Background(), json.RawMessage(input), tc)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Artifacts []ArtifactInfo `json:"artifacts"`
		Note      string         `json:"note"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got.Artifacts, got.Note
}

// sessionArtifacts 按时间顺序返回两张图片和一个视频。
func sessionArtifacts() []ArtifactInfo {
	return []ArtifactInfo{
		{Type: "image", URL: "http://blob/a.png", Tool: "generate_image",
			Description: "按提示词“猫”生成的图片",
			Model:       "z-image-turbo", CreatedAt: "2026-01-01T10:00:00Z", Input: json.RawMessage(`{"prompt":"猫"}`),
			Params: json.RawMessage(`{"aspectRatio":"1:1","size":"1024x1024"}`)},
		{Type: "image", URL: "http://blob/b.png", Tool: "generate_image",
			Model: "z-image-turbo", CreatedAt: "2026-01-01T10:01:00Z", Input: json.RawMessage(`{"prompt":"狗"}`)},
		{Type: "video", URL: "http://blob/c.mp4", Tool: "kling_image_to_video",
			Model: "kling-3.0", CreatedAt: "2026-01-01T10:02:00Z",
			Input: json.RawMessage(`{"prompt":"推近","duration":"5"}`)},
	}
}

func stubListArtifacts() Context {
	return Context{ListArtifacts: func() ([]ArtifactInfo, error) { return sessionArtifacts(), nil }}
}

// TestListArtifactsProjection 验证资源元数据完整投影。
func TestListArtifactsProjection(t *testing.T) {
	arts, note := execListArtifacts(t, stubListArtifacts(), `{}`)
	if len(arts) != 3 || note != "" {
		t.Fatalf("artifacts = %+v, note = %q", arts, note)
	}
	video := arts[2]
	if video.URL != "http://blob/c.mp4" {
		t.Fatalf("video = %+v", video)
	}
	if video.Tool != "kling_image_to_video" || video.Model != "kling-3.0" {
		t.Fatalf("video provenance = %+v", video)
	}
	if arts[0].Description != "按提示词“猫”生成的图片" {
		t.Fatalf("image description = %q", arts[0].Description)
	}
	if !strings.Contains(string(video.Input), "推近") {
		t.Fatalf("video input = %s", video.Input)
	}
	// 实际参数与 tool call 输入分开返回。
	if !strings.Contains(string(arts[0].Params), "1024x1024") {
		t.Fatalf("image params = %s", arts[0].Params)
	}
}

// TestListArtifactsFilterAndLimit 验证类型过滤和最近资源限制。
func TestListArtifactsFilterAndLimit(t *testing.T) {
	images, _ := execListArtifacts(t, stubListArtifacts(), `{"type":"image"}`)
	if len(images) != 2 || images[0].URL != "http://blob/a.png" || images[1].URL != "http://blob/b.png" {
		t.Fatalf("images = %+v", images)
	}

	recent, note := execListArtifacts(t, stubListArtifacts(), `{"limit":1}`)
	if len(recent) != 1 || recent[0].URL != "http://blob/c.mp4" {
		t.Fatalf("recent = %+v", recent)
	}
	if !strings.Contains(note, "最近 1 条") {
		t.Fatalf("note = %q", note)
	}

	withAudio := Context{ListArtifacts: func() ([]ArtifactInfo, error) {
		return append(sessionArtifacts(), ArtifactInfo{
			Type: "audio", URL: "http://blob/d.mp3", Tool: "extract_audio",
		}), nil
	}}
	audio, _ := execListArtifacts(t, withAudio, `{"type":"audio"}`)
	if len(audio) != 1 || audio[0].URL != "http://blob/d.mp3" {
		t.Fatalf("audio = %+v", audio)
	}
}

// TestListArtifactsEmpty 区分 Session 无资源和过滤结果为空。
func TestListArtifactsEmpty(t *testing.T) {
	tc := Context{ListArtifacts: func() ([]ArtifactInfo, error) { return nil, nil }}
	arts, note := execListArtifacts(t, tc, `{}`)
	if len(arts) != 0 || !strings.Contains(note, "还没有已登记的产物") {
		t.Fatalf("artifacts = %+v, note = %q", arts, note)
	}

	// type 过滤为空时返回对应说明。
	onlyImages := Context{ListArtifacts: func() ([]ArtifactInfo, error) {
		return sessionArtifacts()[:2], nil
	}}
	arts, note = execListArtifacts(t, onlyImages, `{"type":"video"}`)
	if len(arts) != 0 || !strings.Contains(note, "去掉 type 再查") {
		t.Fatalf("artifacts = %+v, note = %q", arts, note)
	}

	// 有结果时不返回空列表说明。
	if _, note := execListArtifacts(t, stubListArtifacts(), `{"type":"video","limit":5}`); note != "" {
		t.Fatalf("note = %q, want empty for a non-empty result", note)
	}
}

// TestListArtifactsErrors 覆盖未知类型和缺少 Session 上下文。
func TestListArtifactsErrors(t *testing.T) {
	spec := NewListArtifactsTool()
	if _, err := spec.Run(context.Background(), json.RawMessage(`{"type":"document"}`), stubListArtifacts()); err == nil {
		t.Fatal("unknown type must fail")
	}
	if _, err := spec.Run(context.Background(), json.RawMessage(`{}`), Context{}); err == nil {
		t.Fatal("missing artifact registry must fail")
	}
}

// TestListArtifactsSpec 验证工具 schema 和 InlineBinding。
func TestListArtifactsSpec(t *testing.T) {
	spec := NewListArtifactsTool()
	if spec.Name != ListArtifactsToolName {
		t.Fatalf("spec = %+v", spec)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 0 || len(schema.Properties) != 2 {
		t.Fatalf("schema = %+v", schema)
	}
	for _, name := range []string{"type", "limit"} {
		if _, ok := schema.Properties[name]; !ok {
			t.Fatalf("schema missing %q", name)
		}
	}
	if !strings.Contains(string(schema.Properties["type"]), `"audio"`) {
		t.Fatalf("type schema does not include audio: %s", schema.Properties["type"])
	}
}
