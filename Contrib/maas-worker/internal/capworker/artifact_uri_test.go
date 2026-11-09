package capworker

import "testing"

func TestArtifactURIResolverFormatsBlobResourceHandles(t *testing.T) {
	tests := map[string]struct {
		reference string
		want      string
	}{
		"MaaS reference": {
			reference: "agentk:artifacts:path/video.mp4",
			want:      "blob://agentk-artifacts/path/video.mp4",
		},
		"existing Blob URI": {
			reference: "blob://agentk-artifacts/path/video.mp4",
			want:      "blob://agentk-artifacts/path/video.mp4",
		},
		"existing HTTP URI": {
			reference: "https://blob.test/agentk-artifacts/path/video.mp4",
			want:      "https://blob.test/agentk-artifacts/path/video.mp4",
		},
		"existing local HTTP URI": {
			reference: "http://127.0.0.1:8080/files/outputs/audio.wav",
			want:      "http://127.0.0.1:8080/files/outputs/audio.wav",
		},
	}
	resolver := newArtifactURIResolver()
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := resolver.URI(test.reference)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("URI() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestArtifactURIResolverRejectsUnmanagedOutputs(t *testing.T) {
	resolver := newArtifactURIResolver()
	for _, reference := range []string{
		"outputs/video.mp4",
		"blob:///video.mp4",
		"blob://bucket/../escape.mp4",
		"agentk:artifacts:../escape.mp4",
	} {
		if _, err := resolver.URI(reference); err == nil {
			t.Fatalf("URI(%q) unexpectedly succeeded", reference)
		}
	}
}

func TestAttachManagedArtifactsPreservesRawMaaSReferences(t *testing.T) {
	activity := inferActivity{
		artifactURIs: testArtifactURIResolver(),
		artifactOutputs: map[string]string{
			"instrumental": "audio", "vocals": "audio",
		},
	}
	output, err := activity.attachManagedArtifacts(map[string]any{
		"vocals":           "agentk:artifacts:vocals.wav",
		"instrumental":     "agentk:artifacts:instrumental.wav",
		"duration_seconds": 3.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output["vocals"] != "agentk:artifacts:vocals.wav" {
		t.Fatalf("raw output was changed: %+v", output)
	}
	artifacts := output["artifacts"].([]map[string]any)
	if len(artifacts) != 2 ||
		artifacts[0]["uri"] != "blob://agentk-artifacts/instrumental.wav" ||
		artifacts[1]["uri"] != "blob://agentk-artifacts/vocals.wav" {
		t.Fatalf("artifacts = %+v", artifacts)
	}
}

func TestAttachManagedArtifactsRejectsMalformedReference(t *testing.T) {
	activity := inferActivity{
		artifactURIs:    testArtifactURIResolver(),
		artifactOutputs: map[string]string{"audio": "audio"},
	}
	if _, err := activity.attachManagedArtifacts(map[string]any{"audio": 42}); err == nil {
		t.Fatal("numeric artifact reference unexpectedly succeeded")
	}
}
