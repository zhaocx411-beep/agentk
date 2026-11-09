package app

import (
	"strings"
	"testing"

	"agentk/internal/artifact"
)

func TestAPIConfigFromEnv(t *testing.T) {
	t.Run("standalone memory", func(t *testing.T) {
		setAPIConfigEnv(t, string(ModeStandalone), storeMem, AuthOff)
		cfg, err := APIConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Mode != ModeStandalone || cfg.Store != storeMem || cfg.Auth != AuthOff ||
			cfg.Listen != ":8080" {
			t.Fatalf("config = %+v", cfg)
		}
	})

	t.Run("deployed persistent", func(t *testing.T) {
		setAPIConfigEnv(t, string(ModeDeployed), storeKDB, AuthAPIKey)
		cfg, err := APIConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Mode != ModeDeployed || cfg.Store != storeKDB || cfg.Auth != AuthAPIKey {
			t.Fatalf("config = %+v", cfg)
		}
	})

	t.Run("deployed rejects memory", func(t *testing.T) {
		setAPIConfigEnv(t, string(ModeDeployed), storeMem, AuthOff)
		_, err := APIConfigFromEnv()
		if err == nil || !strings.Contains(err.Error(), "persistent AGENTK_STORE") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("api key rejects memory", func(t *testing.T) {
		setAPIConfigEnv(t, string(ModeStandalone), storeMem, AuthAPIKey)
		_, err := APIConfigFromEnv()
		if err == nil || !strings.Contains(err.Error(), "AGENTK_AUTH=apikey") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestWorkerConfigFromEnv(t *testing.T) {
	for _, store := range []string{storeMySQL, storeKDB} {
		t.Run(store, func(t *testing.T) {
			t.Setenv("AGENTK_STORE", store)
			// API 专属配置即使非法也不能影响独立 Worker。
			t.Setenv("AGENTK_MODE", "invalid")
			t.Setenv("AGENTK_AUTH", "invalid")
			t.Setenv("AGENTK_LISTEN", "invalid")
			cfg, err := WorkerConfigFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Store != store {
				t.Fatalf("store = %q", cfg.Store)
			}
		})
	}

	for _, store := range []string{"", storeMem} {
		t.Run("reject "+store, func(t *testing.T) {
			t.Setenv("AGENTK_STORE", store)
			if _, err := WorkerConfigFromEnv(); err == nil {
				t.Fatalf("AGENTK_STORE=%q unexpectedly accepted", store)
			}
		})
	}
}

func TestValidateArtifactStorage(t *testing.T) {
	local := artifact.NewLocalStorage(t.TempDir(), "http://local.test")
	if err := validateArtifactStorage(ModeStandalone, local); err != nil {
		t.Fatalf("standalone local storage: %v", err)
	}
	if err := validateArtifactStorage(ModeDeployed, local); err == nil {
		t.Fatal("deployed local storage unexpectedly accepted")
	}

	blob, err := artifact.NewBlobStorage(artifact.BlobConfig{
		Endpoint: "http://blob.test",
		Bucket:   "bucket",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateArtifactStorage(ModeDeployed, blob); err != nil {
		t.Fatalf("deployed blob storage: %v", err)
	}
}

func setAPIConfigEnv(t *testing.T, mode, store, auth string) {
	t.Helper()
	t.Setenv("AGENTK_MODE", mode)
	t.Setenv("AGENTK_STORE", store)
	t.Setenv("AGENTK_AUTH", auth)
	t.Setenv("AGENTK_LISTEN", "")
}
