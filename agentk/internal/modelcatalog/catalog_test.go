package modelcatalog

import (
	"strings"
	"testing"

	"agentk/internal/artifact"
	"agentk/internal/capability/tts"
)

func TestParseKnownModelsAndStaticRouting(t *testing.T) {
	raw := []byte(`
version: cat-v1
models:
  - name: deepseek-v4-pro
    baseUrl: https://chat.test/v1
  - name: z-image-turbo
    apiKey: image-key
  - name: minimax-speech-2.8-hd
    apiKey: tts-key
    groupId: group
    voices:
      - id: account-voice
  - name: kling-3.0
`)
	cat, err := Parse(raw, artifact.NewLocalStorage(t.TempDir(), "http://local.test"))
	if err != nil {
		t.Fatal(err)
	}
	if cat.Version != "cat-v1" || len(cat.Chat) != 1 {
		t.Fatalf("catalog = %+v", cat)
	}
	if _, ok := cat.ImageClients["z-image-turbo"]; !ok {
		t.Fatal("image client missing")
	}
	if _, ok := cat.TTSClients["minimax-speech-2.8-hd"]; !ok {
		t.Fatal("tts client missing")
	}
	if _, ok := cat.KlingI2VClients["kling-3.0"]; !ok {
		t.Fatal("video client missing")
	}
}

func TestParseIgnoresUnknownModel(t *testing.T) {
	cat, err := Parse([]byte(`
models:
  - name: invented-model
    baseUrl: https://chat.test
  - name: deepseek-v4-pro
    baseUrl: https://known.test
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Chat) != 1 {
		t.Fatalf("chat models = %d, want 1", len(cat.Chat))
	}
	if _, ok := cat.Chat["deepseek-v4-pro"]; !ok {
		t.Fatal("known model missing")
	}
}

func TestParseRejectsLegacyContractFields(t *testing.T) {
	tests := map[string]string{
		"kind":         `models: [{name: deepseek-v4-pro, kind: chat, baseUrl: https://chat.test}]`,
		"api":          `models: [{name: deepseek-v4-pro, api: openai, baseUrl: https://chat.test}]`,
		"description":  `models: [{name: deepseek-v4-pro, description: test, baseUrl: https://chat.test}]`,
		"defaultModel": `defaultModel: deepseek-v4-pro` + "\n" + `models: [{name: deepseek-v4-pro, baseUrl: https://chat.test}]`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw), nil); err == nil {
				t.Fatal("expected parse error")
			}
		})
	}
}

func TestMissingKnownCapabilityModelDoesNotBlockCatalog(t *testing.T) {
	cat, err := Parse([]byte(`
models:
  - name: deepseek-v4-pro
    baseUrl: https://chat.test/v1
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Version) != 12 {
		t.Fatalf("derived version = %q", cat.Version)
	}
	if _, ok := cat.ImageClients["z-image-turbo"]; ok {
		t.Fatal("unconfigured image model unexpectedly available")
	}
	if _, ok := cat.TTSClients["qwen3-tts-flash"]; ok {
		t.Fatal("unconfigured TTS model unexpectedly available")
	}
}

func TestExplicitEmptyDeploymentCatalogIsValid(t *testing.T) {
	cat, err := Parse([]byte("models: []\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Chat) != 0 || cat.ProductDetailClient != nil {
		t.Fatalf("catalog = %+v", cat)
	}
}

func TestActiveProductDetailClientHotSwap(t *testing.T) {
	storage := artifact.NewLocalStorage(t.TempDir(), "http://local.test")
	parse := func(token string) *Catalog {
		cat, err := Parse([]byte(`
models: []
apify:
  token: `+token+`
  taobao_actor: taobao~detail
  amazon_actor: amazon~detail
`), storage)
		if err != nil {
			t.Fatal(err)
		}
		return cat
	}
	active := NewActive(parse("token-one"))
	first, ok := active.ProductDetailClient()
	if !ok {
		t.Fatal("product detail client missing")
	}
	active.Swap(parse("token-two"))
	second, ok := active.ProductDetailClient()
	if !ok || first == second {
		t.Fatalf("client hot swap failed: first=%p second=%p", first, second)
	}
}

func TestParseAllowsCatalogWithoutChatModel(t *testing.T) {
	cat, err := Parse([]byte(`
models:
  - name: volc-search
    apiKey: search-key
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Chat) != 0 {
		t.Fatalf("chat models = %v", cat.Chat)
	}
}

func TestVoiceRestrictionUsesConfiguredAccountResources(t *testing.T) {
	cat, err := Parse([]byte(`
models:
  - name: minimax-speech-2.8-hd
    apiKey: tts-key
    groupId: group
    voices:
      - id: account-voice
`), artifact.NewLocalStorage(t.TempDir(), "http://local.test"))
	if err != nil {
		t.Fatal(err)
	}
	client, ok := cat.TTSClients["minimax-speech-2.8-hd"]
	if !ok {
		t.Fatal("tts client missing")
	}
	if err := client.ValidateRequest(tts.Request{Text: "hello", VoiceID: "other"}); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("voice validation error = %v", err)
	}
}

func TestEditRuntimeConstraintStaysInClientEntry(t *testing.T) {
	cat, err := Parse([]byte(`
models:
  - name: qwen-image-edit-plus-2025-10-30
    baseUrl: https://image.test/api/v1
    headers:
      x-api-key: test
    maxReferenceImages: 2
`), artifact.NewLocalStorage(t.TempDir(), "http://local.test"))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cat.EditClients["qwen-image-edit-plus-2025-10-30"]
	if !ok || entry.MaxReferenceImages != 2 {
		t.Fatalf("edit entry = %+v, ok=%v", entry, ok)
	}
}

func TestActiveSwapsOnlyCatalog(t *testing.T) {
	first, err := Parse([]byte(`
version: one
models: [{name: deepseek-v4-pro, baseUrl: https://chat-one.test}]
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse([]byte(`
version: two
models: [{name: qwen3.5-plus, baseUrl: https://chat-two.test}]
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	active := NewActive(first)
	active.Swap(second)
	if active.Current().Version != "two" {
		t.Fatalf("active catalog = %+v", active.Current())
	}
}
