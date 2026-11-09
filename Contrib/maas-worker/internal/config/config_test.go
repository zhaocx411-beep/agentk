package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadGatewayFromEnvironment(t *testing.T) {
	t.Setenv("MAAS_ROUTES_JSON", `{"routes":{"transnet":{"serviceName":"maas-transnet","timeoutSeconds":600,"allowedFeatures":["video"],"defaultFeatures":{"video":"default:key:value"}}}}`)
	t.Setenv("MAAS_HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("MAAS_MAX_REQUEST_BYTES", "1024")
	t.Setenv("MAAS_SHUTDOWN_TIMEOUT", "10s")
	t.Setenv("MAAS_GATEWAY_BEARER_TOKEN", "secret")

	cfg, err := LoadGateway()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddress != "127.0.0.1:9090" || cfg.MaxRequestSize != 1024 || cfg.Shutdown != 10*time.Second || cfg.BearerToken != "secret" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	route := cfg.Routes["transnet"]
	if route.Timeout() != 10*time.Minute || !route.AllowsFeature("video") || route.AllowsFeature("audio") {
		t.Fatalf("unexpected route: %+v", route)
	}
	if string(route.DefaultFeatures["video"]) != `"default:key:value"` {
		t.Fatalf("default features = %+v", route.DefaultFeatures)
	}
}

func TestExampleGatewayRoutesAllowFFmpegRefs(t *testing.T) {
	t.Setenv("MAAS_ROUTES_KCONF_KEY", "")
	t.Setenv("MAAS_ROUTES_JSON", "")
	t.Setenv("MAAS_ROUTES_FILE", "../../configs/routes.example.json")

	cfg, err := LoadGateway()
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg := cfg.Routes["ffmpeg"]
	if ffmpeg.ServiceName == "" || !ffmpeg.AllowsFeature("refs") ||
		!ffmpeg.AllowsFeature("pipeline") || len(ffmpeg.DefaultFeatures) != 2 {
		t.Fatalf("ffmpeg route = %+v", ffmpeg)
	}
}

func TestLoadGatewayRejectsCapabilityMetadata(t *testing.T) {
	t.Setenv("MAAS_ROUTES_JSON", `{"routes":{"transnet":{"serviceName":"maas-transnet","timeoutSeconds":60,"agentk":{}}}}`)
	_, err := LoadGateway()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadGatewayRejectsInvalidDefaultFeatures(t *testing.T) {
	for name, document := range map[string]string{
		"not allowed": `{"routes":{"ffmpeg":{"serviceName":"maas-ffmpeg","timeoutSeconds":60,"allowedFeatures":["op"],"defaultFeatures":{"db":"agentk"}}}}`,
		"null":        `{"routes":{"ffmpeg":{"serviceName":"maas-ffmpeg","timeoutSeconds":60,"allowedFeatures":["db"],"defaultFeatures":{"db":null}}}}`,
		"reserved":    `{"routes":{"ffmpeg":{"serviceName":"maas-ffmpeg","timeoutSeconds":60,"defaultFeatures":{"request_id":"fixed"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MAAS_ROUTES_JSON", document)
			_, err := LoadGateway()
			if err == nil || !strings.Contains(err.Error(), "default feature") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadGatewayFromKConf(t *testing.T) {
	stubKConf(t, func(key string) (string, error) {
		if key != "kling.maas.gatewayRoutes" {
			t.Fatalf("key = %q", key)
		}
		return `{"routes":{"transnet":{"serviceName":"maas-from-kconf","timeoutSeconds":60}}}`, nil
	})
	t.Setenv("MAAS_ROUTES_KCONF_KEY", "kling.maas.gatewayRoutes")
	t.Setenv("MAAS_ROUTES_JSON", `{"routes":{"ignored":{"serviceName":"ignored","timeoutSeconds":1}}}`)
	cfg, err := LoadGateway()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routes["transnet"].ServiceName != "maas-from-kconf" || len(cfg.Routes) != 1 {
		t.Fatalf("routes = %+v", cfg.Routes)
	}
}

func TestLoadWorkerFromKConf(t *testing.T) {
	stubKConf(t, func(key string) (string, error) {
		return `{"capabilities":{"transnet":{"timeoutSeconds":60,"agentk":{"id":"contrib.maas.transnet","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`, nil
	})
	t.Setenv("MAAS_CAPABILITIES_KCONF_KEY", "kling.maas.stagingCapabilities")
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.internal")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Capabilities["transnet"].AgentK.ID != "contrib.maas.transnet" {
		t.Fatalf("capabilities = %+v", cfg.Capabilities)
	}
}

func TestKConfFailureDoesNotSilentlyUseEnvironmentFallback(t *testing.T) {
	stubKConf(t, func(string) (string, error) { return "", errors.New("kess unavailable") })
	t.Setenv("MAAS_ROUTES_KCONF_KEY", "kling.maas.gatewayRoutes")
	t.Setenv("MAAS_ROUTES_JSON", `{"routes":{"fallback":{"serviceName":"fallback","timeoutSeconds":1}}}`)
	_, err := LoadGateway()
	if err == nil || !strings.Contains(err.Error(), "kess unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerSeparatesCapabilityFromRPCRoute(t *testing.T) {
	t.Setenv("MAAS_CAPABILITIES_JSON", `{
  "capabilities": {
    "transnet": {
      "timeoutSeconds": 60,
      "agentk": {
        "id": "contrib.maas.transnet",
        "description": "detect shots",
        "toolDescription": "use for shot detection",
        "maxAttempts": 2,
        "inputSchema": {"type":"object","required":["video"]},
        "outputSchema": {"type":"object"}
      }
    }
  }
}`)
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://maas-prt.example.test/base")
	t.Setenv("MAAS_HTTP_ADDR", "127.0.0.1:9091")

	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TaskQueue != "maas-worker-staging-agentk" || cfg.GatewayBaseURL.String() != "https://maas-prt.example.test/base" || cfg.HealthAddress != "127.0.0.1:9091" {
		t.Fatalf("unexpected worker config: %+v", cfg)
	}
	if cfg.Capabilities["transnet"].AgentK.ID != "contrib.maas.transnet" {
		t.Fatalf("unexpected capabilities: %+v", cfg.Capabilities)
	}
}

func TestLoadWorkerAcceptsFFmpegGatewayAdapter(t *testing.T) {
	t.Setenv("MAAS_CAPABILITIES_JSON", `{
  "capabilities": {
    "compose-video": {
      "timeoutSeconds": 960,
      "gatewayRoute": "ffmpeg",
      "adapter": "compose_video",
      "agentk": {
        "id": "contrib.maas.compose_video",
        "description": "compose videos",
        "inputSchema": {"type":"object","required":["videoUrls"]},
        "outputSchema": {"type":"object"}
      }
    }
  }
	}`)
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://maas-prt.example.test")

	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	capability := cfg.Capabilities["compose-video"]
	if capability.GatewayRoute != "ffmpeg" || capability.Adapter != "compose_video" {
		t.Fatalf("capabilities = %+v", cfg.Capabilities)
	}
}

func TestLoadWorkerAcceptsProcessVideoGatewayAdapter(t *testing.T) {
	t.Setenv("MAAS_CAPABILITIES_JSON", `{
  "capabilities": {
    "process-video": {
      "timeoutSeconds": 660,
      "gatewayRoute": "ffmpeg",
      "adapter": "process_video",
      "agentk": {
        "id": "contrib.maas.process_video",
        "description": "process video",
        "inputSchema": {"type":"object","required":["videoUrl","operations"]},
        "outputSchema": {"type":"object"}
      }
    }
  }
	}`)
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://maas-prt.example.test")

	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	capability := cfg.Capabilities["process-video"]
	if capability.GatewayRoute != "ffmpeg" || capability.Adapter != "process_video" {
		t.Fatalf("capabilities = %+v", cfg.Capabilities)
	}
}

func TestLoadWorkerRejectsReservedArtifactOutputFeature(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"audio":{"timeoutSeconds":60,"artifactOutputs":{"artifacts":"audio"},"agentk":{"id":"contrib.maas.audio","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "reserved artifact output feature") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerRejectsInvalidArtifactOutputType(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"audio":{"timeoutSeconds":60,"artifactOutputs":{"audio":"Audio File"},"agentk":{"id":"contrib.maas.audio","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "invalid type") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerRejectsUnknownAdapter(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"bad":{"timeoutSeconds":60,"gatewayRoute":"ffmpeg","adapter":"shell","agentk":{"id":"contrib.maas.bad","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "unsupported adapter") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerRequiresGatewayRouteForAdapter(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"extract-audio":{"timeoutSeconds":60,"adapter":"extract_audio","agentk":{"id":"contrib.maas.extract_audio","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "requires gatewayRoute") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerRequiresHTTPS(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "http://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerAllowsHTTPOnlyWhenExplicitlyEnabled(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_GATEWAY_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("MAAS_GATEWAY_ALLOW_INSECURE_HTTP", "on")
	if _, err := LoadWorker(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWorkerRejectsRPCFields(t *testing.T) {
	setMinimalWorkerEnvironment(t)
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"transnet":{"serviceName":"prt-rpc","timeoutSeconds":60,"agentk":{"id":"contrib.maas.transnet","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadWorkerRejectsDuplicateAgentKIdentity(t *testing.T) {
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{
  "route-a":{"timeoutSeconds":60,"agentk":{"id":"contrib.maas.same","description":"a","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}},
  "route_a":{"timeoutSeconds":60,"agentk":{"id":"contrib.maas.same","description":"b","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}
}}`)
	t.Setenv("KWS_SERVICE_NAME", "maas-worker")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.example.test")
	_, err := LoadWorker()
	if err == nil || !strings.Contains(err.Error(), "repeat agentk.id") {
		t.Fatalf("error = %v", err)
	}
}

func TestAgentKActivityTypeNormalizesRouteName(t *testing.T) {
	if got := AgentKActivityType("minimax-h3"); got != "maas.infer.minimax_h3.v1" {
		t.Fatalf("activity type = %q", got)
	}
}

func setMinimalWorkerEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("MAAS_CAPABILITIES_JSON", `{"capabilities":{"transnet":{"timeoutSeconds":60,"agentk":{"id":"contrib.maas.transnet","description":"x","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}}}}`)
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
}

func stubKConf(t *testing.T, get func(string) (string, error)) {
	t.Helper()
	original := getKConfString
	getKConfString = get
	t.Cleanup(func() { getKConfString = original })
}
