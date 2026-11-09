package seedance_test

import (
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/seedance"
)

func TestModelLimits(t *testing.T) {
	for _, tc := range []struct {
		name             string
		change           func(*seedance.Request)
		valid20, valid25 bool
	}{
		{"30 seconds", func(r *seedance.Request) { r.Duration = 30 }, false, true},
		{"4k", func(r *seedance.Request) { r.Resolution = "4k" }, true, false},
		{"audio only", func(r *seedance.Request) { r.Media = []seedance.Media{{Role: "reference_audio"}} }, false, true},
		{"video duration", func(r *seedance.Request) { r.Duration = -1; r.Media = []seedance.Media{{Role: "reference_video"}} }, false, true},
		{"missing video", func(r *seedance.Request) { r.Duration = -1 }, false, false},
		{"first frame ratio", func(r *seedance.Request) { r.Ratio = "16:9"; r.Media = []seedance.Media{{Role: "first_frame"}} }, true, false},
		{"30 images", func(r *seedance.Request) {
			for range 30 {
				r.Media = append(r.Media, seedance.Media{Role: "reference_image"})
			}
		}, false, true},
		{"total media", func(r *seedance.Request) {
			for role, count := range map[string]int{"reference_image": 30, "reference_video": 10, "reference_audio": 10, "first_frame": 1} {
				for range count {
					r.Media = append(r.Media, seedance.Media{Role: role})
				}
			}
		}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for model, valid := range map[string]bool{"endpoint-2.0": tc.valid20, "doubao-seedance-2.5": tc.valid25} {
				r := seedance.Request{Model: model, Prompt: "p", Duration: 5, Resolution: "1080p", Ratio: "adaptive"}
				tc.change(&r)
				if err := r.Validate(); (err == nil) != valid {
					t.Errorf("model=%s error=%v", model, err)
				}
			}
		})
	}
}
