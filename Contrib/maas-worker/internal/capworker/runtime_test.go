package capworker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
	"go.temporal.io/sdk/temporal"
)

func TestDefinitionsForRoutes(t *testing.T) {
	routes := map[string]config.CapabilityRoute{
		"minimax-h3": {
			TimeoutSeconds:  1800,
			ArtifactOutputs: map[string]string{"video": "video"},
			AgentK: config.AgentKCapability{
				ID:              "contrib.maas.minimax_h3",
				Description:     "generate video",
				ToolDescription: "use for video generation",
				InputSchema:     json.RawMessage(`{"type":"object"}`),
				OutputSchema:    json.RawMessage(`{"type":"object"}`),
				MaxAttempts:     0,
			},
		},
	}
	definitions := definitionsForRoutes(routes)
	if len(definitions) != 1 {
		t.Fatalf("definitions = %+v", definitions)
	}
	definition := definitions[0]
	if definition.route != "minimax-h3" || definition.adapter != "" ||
		definition.temporalType != "maas.infer.minimax_h3.v1" {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.artifactOutputs["video"] != "video" {
		t.Fatalf("artifact outputs = %+v", definition.artifactOutputs)
	}
	if definition.capability.Tool == nil || definition.capability.Tool.MaxAttempts != 1 {
		t.Fatalf("default max attempts = %+v", definition.capability.Tool)
	}
	manifest := sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       "maas-worker-test",
		Capabilities: []sdkcapability.CapabilityDef{{
			ID: definition.capability.ID, Kind: sdkcapability.CapabilityKindActivity,
			TemporalType:   definition.capability.TemporalType,
			Description:    definition.capability.Description,
			InputSchema:    definition.capability.InputSchema,
			OutputSchema:   definition.capability.OutputSchema,
			TimeoutSeconds: definition.capability.TimeoutSeconds,
			Tool: &sdkcapability.ToolDecl{
				Description: definition.capability.Tool.Description,
				Mode:        sdkcapability.ToolModeBlocking,
				MaxAttempts: definition.capability.Tool.MaxAttempts,
			},
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExampleCapabilitiesManifestIsValid(t *testing.T) {
	t.Setenv("MAAS_CAPABILITIES_FILE", "../../configs/capabilities.example.json")
	t.Setenv("KWS_SERVICE_NAME", "maas-worker-staging")
	t.Setenv("MAAS_GATEWAY_BASE_URL", "https://gateway.internal")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	definitions := definitionsForRoutes(cfg.Capabilities)
	if len(definitions) != 9 {
		t.Fatalf("capabilities = %d, want 9", len(definitions))
	}

	manifest := sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       cfg.TaskQueue,
		Capabilities:    make([]sdkcapability.CapabilityDef, 0, len(definitions)),
	}
	for _, definition := range definitions {
		item := sdkcapability.CapabilityDef{
			ID: definition.capability.ID, Kind: sdkcapability.CapabilityKindActivity,
			TemporalType:   definition.capability.TemporalType,
			Description:    definition.capability.Description,
			InputSchema:    definition.capability.InputSchema,
			OutputSchema:   definition.capability.OutputSchema,
			TimeoutSeconds: definition.capability.TimeoutSeconds,
		}
		if tool := definition.capability.Tool; tool != nil {
			item.Tool = &sdkcapability.ToolDecl{
				Description: tool.Description,
				Mode:        sdkcapability.ToolModeBlocking,
				MaxAttempts: tool.MaxAttempts,
			}
		}
		manifest.Capabilities = append(manifest.Capabilities, item)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDefinitionsSelectGatewayAdapter(t *testing.T) {
	routes := map[string]config.CapabilityRoute{
		"compose-video": {
			TimeoutSeconds: 960,
			GatewayRoute:   "ffmpeg",
			Adapter:        "compose_video",
			AgentK: config.AgentKCapability{
				ID: "contrib.maas.compose_video", Description: "compose",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	definitions := definitionsForRoutes(routes)
	if len(definitions) != 1 || definitions[0].route != "ffmpeg" ||
		definitions[0].adapter != "compose_video" {
		t.Fatalf("definitions = %+v", definitions)
	}
}

func TestDefinitionsSelectProcessVideoAdapter(t *testing.T) {
	routes := map[string]config.CapabilityRoute{
		"process-video": {
			TimeoutSeconds: 660,
			GatewayRoute:   "ffmpeg",
			Adapter:        "process_video",
			AgentK: config.AgentKCapability{
				ID: "contrib.maas.process_video", Description: "process video",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				OutputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	definitions := definitionsForRoutes(routes)
	if len(definitions) != 1 || definitions[0].route != "ffmpeg" ||
		definitions[0].adapter != "process_video" {
		t.Fatalf("definitions = %+v", definitions)
	}
}

type failingGateway struct{ err error }

func (f failingGateway) Infer(context.Context, string, string, map[string]any) (map[string]any, error) {
	return nil, f.err
}

func TestInferActivityMakesPermanentGatewayErrorsNonRetryable(t *testing.T) {
	activity := inferActivity{
		route: "transnet",
		gateway: failingGateway{err: &CallError{
			StatusCode: 400, Code: "INVALID", Message: "bad input", Permanent: true,
		}},
	}
	_, err := activity.Run(t.Context(), map[string]any{"video": "x"})
	var applicationError *temporal.ApplicationError
	if !errors.As(err, &applicationError) || !applicationError.NonRetryable() {
		t.Fatalf("error = %#v", err)
	}
}

func TestInferActivityAttachesManagedArtifactsToDirectGatewayOutput(t *testing.T) {
	gateway := &scriptedGateway{responses: []map[string]any{{
		"audio": "agentk:artifacts:speech.wav", "duration_seconds": 1.5,
	}}}
	activity := inferActivity{
		route: "indextts", gateway: gateway,
		artifactURIs:    testArtifactURIResolver(),
		artifactOutputs: map[string]string{"audio": "audio"},
	}
	output, err := activity.Run(t.Context(), map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := output["artifacts"].([]map[string]any)
	if len(artifacts) != 1 ||
		artifacts[0]["uri"] != "blob://agentk-artifacts/speech.wav" ||
		output["audio"] != "agentk:artifacts:speech.wav" {
		t.Fatalf("output = %+v", output)
	}
}
