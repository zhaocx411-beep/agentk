package capability

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestArtifactJSONWire(t *testing.T) {
	tests := []struct {
		name     string
		artifact Artifact
		want     string
	}{
		{
			name:     "required fields",
			artifact: Artifact{Type: "video", URI: "kfs://outputs/video.mp4"},
			want:     `{"type":"video","uri":"kfs://outputs/video.mp4"}`,
		},
		{
			name: "all fields",
			artifact: Artifact{
				Type:        "video",
				URI:         "kfs://outputs/video.mp4",
				Description: "品牌广告成片",
				Model:       "kling-video",
				Params:      map[string]any{"duration": 5},
			},
			want: `{"type":"video","uri":"kfs://outputs/video.mp4","description":"品牌广告成片","model":"kling-video","params":{"duration":5}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.artifact)
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected any
			if err := json.Unmarshal(got, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.want), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("artifact JSON = %s, want %s", got, tt.want)
			}
		})
	}
}
