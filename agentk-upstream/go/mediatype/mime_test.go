package mediatype

import "testing"

func TestCanonicalAudioAliases(t *testing.T) {
	tests := map[string]string{
		"Audio/X-Wav; charset=binary": "audio/wav",
		"audio/x-m4a":                 "audio/m4a",
		"audio/mp4":                   "audio/m4a",
		"audio/x-flac":                "audio/flac",
	}
	for input, want := range tests {
		if got := Canonical(input); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWebMExtensionIsAmbiguous(t *testing.T) {
	if got, ambiguous := FromExtension("clip.webm"); got != "" || !ambiguous {
		t.Fatalf("FromExtension(webm) = %q, %v", got, ambiguous)
	}
}

func TestSupportedAudioMIMEs(t *testing.T) {
	for _, mimeType := range []string{
		"audio/wav", "audio/mp3", "audio/aiff", "audio/aac", "audio/ogg",
		"audio/flac", "audio/mpeg", "audio/m4a", "audio/l16", "audio/opus",
		"audio/alaw", "audio/mulaw", "audio/webm",
	} {
		if !SupportedAudio(mimeType) {
			t.Errorf("%s is not supported", mimeType)
		}
	}
}

func TestAudioExtensions(t *testing.T) {
	for extension, want := range map[string]string{
		".wav": "audio/wav", ".mp3": "audio/mpeg", ".aif": "audio/aiff",
		".aiff": "audio/aiff", ".aac": "audio/aac", ".ogg": "audio/ogg",
		".oga": "audio/ogg", ".flac": "audio/flac", ".mpga": "audio/mpeg",
		".m4a": "audio/m4a", ".l16": "audio/l16", ".opus": "audio/opus",
		".alaw": "audio/alaw", ".mulaw": "audio/mulaw",
	} {
		got, ambiguous := FromExtension("speech" + extension)
		if ambiguous || got != want {
			t.Errorf("FromExtension(%q) = %q, %v; want %q", extension, got, ambiguous, want)
		}
	}
}
