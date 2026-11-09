package asr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

func TestAzureTranscribeAndTranslate(t *testing.T) {
	var gotAPIKey, gotModel, gotLanguage, gotPrompt, gotFilename, gotFormat, gotGranularity string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/clip.wav", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF\x00\x00\x00\x00WAVE"))
	})
	mux.HandleFunc("/openai/deployments/whisper/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api-version") != DefaultAzureAPIVersion {
			t.Errorf("api-version = %q", r.URL.Query().Get("api-version"))
		}
		gotAPIKey = r.Header.Get("api-key")
		if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
			t.Fatal(err)
		}
		gotModel, gotLanguage, gotPrompt = r.FormValue("model"), r.FormValue("language"), r.FormValue("prompt")
		gotFormat, gotGranularity = r.FormValue("response_format"), r.FormValue("timestamp_granularities[]")
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		gotFilename = header.Filename
		_ = file.Close()
		w.Header().Set("apim-request-id", "request-1")
		_, _ = w.Write([]byte(`{"text":"你好，世界","language":"chinese","duration":2.5,` +
			`"segments":[{"id":0,"start":0.0,"end":2.5,"text":"你好，世界"}]}`))
	})
	mux.HandleFunc("/openai/deployments/whisper/audio/translations", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"text":"Hello, world"}`))
	})
	client, err := NewAzure(AzureConfig{
		BaseURL: srv.URL, APIKey: "secret", Deployment: "whisper", Model: "whisper-model",
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.allowPrivateFetch = true
	result, err := client.Transcribe(context.Background(), Request{
		AudioURL: srv.URL + "/clip.wav", Language: "zh", Prompt: "专有名词",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "你好，世界" || result.Metadata.RequestID != "request-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Language != "chinese" || result.Duration != 2.5 || len(result.Segments) != 1 ||
		result.Segments[0].Start != 0 || result.Segments[0].End != 2.5 {
		t.Fatalf("timeline = %+v", result)
	}
	if gotAPIKey != "secret" || gotModel != "whisper-model" || gotLanguage != "zh" ||
		gotPrompt != "专有名词" || gotFilename != "clip.wav" ||
		gotFormat != ResponseFormatVerboseJSON || gotGranularity != "segment" {
		t.Fatalf("key=%q model=%q language=%q prompt=%q filename=%q format=%q granularity=%q",
			gotAPIKey, gotModel, gotLanguage, gotPrompt, gotFilename, gotFormat, gotGranularity)
	}
	translated, err := client.Transcribe(context.Background(), Request{
		AudioURL: srv.URL + "/clip.wav", Task: TaskTranslate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if translated.Text != "Hello, world" || translated.Params["task"] != TaskTranslate {
		t.Fatalf("translated = %+v", translated)
	}
}

func TestAzureReadsManagedArtifact(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifact.test")
	object, err := storage.Put(context.Background(), "input/audio.wav",
		[]byte("RIFF\x00\x00\x00\x00WAVE"), "audio/wav")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxAudioBytes); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("response_format") != ResponseFormatJSON ||
			r.FormValue("timestamp_granularities[]") != "" {
			t.Fatalf("format=%q granularity=%q",
				r.FormValue("response_format"), r.FormValue("timestamp_granularities[]"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(file)
		_ = file.Close()
		if !strings.HasPrefix(string(data), "RIFF") {
			t.Errorf("data = %q", data)
		}
		_, _ = w.Write([]byte(`{"text":"managed"}`))
	}))
	t.Cleanup(srv.Close)
	client, err := NewAzure(AzureConfig{
		BaseURL: srv.URL, APIKey: "key", Deployment: "whisper",
		ResponseFormat: ResponseFormatJSON, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Transcribe(context.Background(), Request{AudioURL: object.URL})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "managed" {
		t.Fatalf("result = %+v", result)
	}
}

func TestAzureErrorsAndValidation(t *testing.T) {
	client, err := NewAzure(AzureConfig{
		BaseURL: "https://azure.test", APIKey: "key", Deployment: "whisper",
		Client: http.DefaultClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	safeClient, err := NewAzure(AzureConfig{
		BaseURL: "https://azure.test", APIKey: "key", Deployment: "whisper",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := safeClient.Transcribe(context.Background(), Request{
		AudioURL: "http://127.0.0.1/private.wav",
	}); err == nil || !strings.Contains(err.Error(), "unsafe audioUrl") {
		t.Fatalf("private audio URL should be rejected, got %v", err)
	}
	for _, req := range []Request{
		{},
		{AudioURL: "https://x/a.wav", Task: "summarize"},
		{AudioURL: "https://x/a.wav", Task: TaskTranslate, Language: "zh"},
	} {
		if _, err := normalizeRequest(req); err == nil {
			t.Fatalf("expected validation error for %+v", req)
		}
	}
	for i, cfg := range []AzureConfig{
		{BaseURL: "relative", APIKey: "k", Deployment: "d"},
		{BaseURL: "https://x", Deployment: "d"},
		{BaseURL: "https://x", APIKey: "k"},
		{BaseURL: "https://x", APIKey: "k", Deployment: "d", ProxyURL: "relative"},
		{BaseURL: "https://x", APIKey: "k", Deployment: "d", ResponseFormat: "diarized_json"},
	} {
		if _, err := NewAzure(cfg); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
	_ = client
}

func TestReadAudioRejectsInvalidInput(t *testing.T) {
	if _, err := readAudio(strings.NewReader(""), "empty.wav", "audio/wav"); err == nil {
		t.Fatal("expected empty audio error")
	}
	if _, err := readAudio(strings.NewReader("text"), "note.txt", "text/plain"); err == nil {
		t.Fatal("expected unsupported audio error")
	}
}
