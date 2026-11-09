package tool

import (
	"encoding/json"
	"testing"
)

func TestExtractResultArtifacts(t *testing.T) {
	artifacts := ExtractResultArtifacts(json.RawMessage(`{
		"artifacts": [
			{
				"type": "video",
				"uri": "https://cdn.example.com/video.mp4",
				"description": "测试成片",
				"model": "video-v1",
				"params": {"duration": "5"}
			},
			{"type": "image"},
			{"type": "image", "uri": "https://cdn.example.com/cover.png"}
		]
	}`))
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	got := artifacts[0]
	if got.Type != "video" || got.URI != "https://cdn.example.com/video.mp4" ||
		got.Description != "测试成片" || got.Model != "video-v1" ||
		string(got.Params) != `{"duration": "5"}` {
		t.Fatalf("artifact = %+v", got)
	}
	if artifacts[1].Type != "image" || artifacts[1].URI != "https://cdn.example.com/cover.png" {
		t.Fatalf("second artifact = %+v", artifacts[1])
	}
}

func TestExtractResultArtifactsRejectsMissingContractFields(t *testing.T) {
	for _, result := range []string{
		`{}`,
		`{"artifacts": null}`,
		`{"artifacts": []}`,
		`{"artifact": {"type": "image", "uri": "https://cdn.example.com/old.png"}}`,
		`{"artifacts": [{"type": "image"}]}`,
		`{"artifacts": [{"uri": "https://cdn.example.com/image.png"}]}`,
		`{"artifacts": [{"type": " ", "uri": "https://cdn.example.com/image.png"}]}`,
	} {
		if got := ExtractResultArtifacts(json.RawMessage(result)); got != nil {
			t.Fatalf("result %s: artifacts = %+v, want nil", result, got)
		}
	}
}
