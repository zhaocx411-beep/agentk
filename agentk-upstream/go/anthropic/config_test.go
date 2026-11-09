package anthropic

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway/klink"
)

func TestConfig(t *testing.T) {
	temperature := 0.0
	cfg := Config{MaxTokens: 1024, CacheTTL: "5m", Temperature: &temperature}
	req := chat.Request{Model: "claude-haiku-4-5-20251001", Messages: []chat.Message{{Role: chat.RoleUser, Text: "hello"}}, OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}
	for _, ttl := range []string{"5m", "1h"} {
		cfg.CacheTTL = ttl
		p, err := requestParams(req, cfg)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if body["temperature"] != float64(0) || body["cache_control"].(map[string]any)["ttl"] != ttl {
			t.Fatalf("missing settings: %s", raw)
		}
	}
	temperature = 1
	cfg.Effort = "medium"
	req.Model = "claude-sonnet-5"
	p, err := requestParams(req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.OutputConfig.Effort != "medium" || p.OutputConfig.Format.Schema["type"] != "object" {
		t.Fatal("effort replaced schema")
	}
	raw, _ := json.Marshal(p)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	if _, present := body["temperature"]; present {
		t.Fatal("adaptive model received deprecated temperature")
	}
	for _, bad := range []Config{
		{MaxTokens: 0}, {MaxTokens: 1, CacheTTL: "10m"}, {MaxTokens: 1, Effort: "unknown"},
		{MaxTokens: 1, Temperature: new(-0.1)}, {MaxTokens: 1, Temperature: new(1.1)}, {MaxTokens: 1, Temperature: new(math.NaN())},
	} {
		if _, err := NewChat(klink.Anthropic("test"), bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	for _, tc := range []struct {
		model  string
		config Config
	}{
		{"claude-haiku-4-5-20251001", Config{MaxTokens: 1024, Effort: "low"}},
		{"claude-sonnet-5", Config{MaxTokens: 1024, Temperature: new(0.5)}},
	} {
		c, err := NewChat(klink.Anthropic("test"), tc.config)
		if err != nil {
			t.Fatal(err)
		}
		req.Model = tc.model
		if _, err := c.Stream(t.Context(), req); !errors.Is(err, chat.ErrRejected) {
			t.Fatalf("expected non-retryable error: %v", err)
		}
	}
}
