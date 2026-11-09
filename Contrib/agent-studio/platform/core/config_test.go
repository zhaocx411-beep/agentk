package core

import "testing"

func TestLoadConfigIgnoresLegacyAgentKBaseURL(t *testing.T) {
	t.Setenv("API_PORT", "")
	t.Setenv("DRAMA_MODE", "")
	t.Setenv("AGENTK_BASE_URL", "https://agentk-api.staging.kuaishou.com")
	t.Setenv("AGENTK_LOCAL_BASE_URL", "")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentKLocalBaseURL != "" {
		t.Fatalf("AgentKLocalBaseURL = %q, want empty", config.AgentKLocalBaseURL)
	}
}

func TestLoadConfigAcceptsLocalAgentKOverride(t *testing.T) {
	t.Setenv("API_PORT", "")
	t.Setenv("DRAMA_MODE", "")
	t.Setenv("AGENTK_LOCAL_BASE_URL", "  http://127.0.0.1:18081/  ")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.AgentKLocalBaseURL != "http://127.0.0.1:18081/" {
		t.Fatalf("AgentKLocalBaseURL = %q", config.AgentKLocalBaseURL)
	}
}
