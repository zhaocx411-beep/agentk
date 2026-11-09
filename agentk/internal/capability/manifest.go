package capability

import (
	"encoding/json"
	"fmt"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"

	"agentk/internal/capability/asr"
	"agentk/internal/capability/audioextract"
	"agentk/internal/capability/brandad"
	"agentk/internal/capability/frameextract"
	"agentk/internal/capability/imagegen"
	"agentk/internal/capability/mediaunderstand"
	"agentk/internal/capability/readfile"
	"agentk/internal/capability/textgen"
	"agentk/internal/capability/tts"
	"agentk/internal/capability/videocompose"
	"agentk/internal/capability/videogen"
	"agentk/internal/capability/videoprobe"
	"agentk/internal/capability/videoprocess"
	"agentk/internal/capability/webfetch"
	"agentk/internal/capability/websearch"
	"agentk/internal/tool"
)

const coreTaskQueue = "agentk"

// CoreManifest 返回随 AgentK 发布、直接写入静态 Catalog 的内置能力契约。
// 这些能力不经过 Worker announce。
func CoreManifest() (sdkcapability.ManifestV1, error) {
	defs := make([]sdkcapability.CapabilityDef, 0, 22)
	brand, err := capabilityDef(
		sdkcapability.CapabilityKindWorkflow,
		brandad.ManifestCapability(),
	)
	if err != nil {
		return sdkcapability.ManifestV1{}, err
	}
	defs = append(defs, brand)

	fixedOutputs := []struct {
		id     string
		output json.RawMessage
	}{
		{"agentk.compose_video", sdkcapability.SchemaOf[videocompose.ComposeActivityOutput]()},
		{"agentk.probe_video", sdkcapability.SchemaOf[videoprobe.Output]()},
		{"agentk.process_video", sdkcapability.SchemaOf[videoprocess.ActivityOutput]()},
		{"agentk.extract_audio", sdkcapability.SchemaOf[audioextract.ActivityOutput]()},
		{"agentk.extract_frame", sdkcapability.SchemaOf[frameextract.ActivityOutput]()},
		{"agentk.read_file", sdkcapability.SchemaOf[readfile.ActivityOutput]()},
	}
	for i, spec := range fixedMediaTools() {
		item := fixedOutputs[i]
		binding, ok := spec.Binding.(tool.ActivityBinding)
		if !ok {
			return sdkcapability.ManifestV1{}, fmt.Errorf(
				"core capability %q must use an activity binding", item.id)
		}
		defs = append(defs, sdkcapability.CapabilityDef{
			ID:             item.id,
			Kind:           sdkcapability.CapabilityKindActivity,
			TemporalType:   binding.ActivityName,
			Description:    spec.Description,
			InputSchema:    append(json.RawMessage(nil), spec.InputSchema...),
			OutputSchema:   append(json.RawMessage(nil), item.output...),
			TimeoutSeconds: int64(binding.StartToCloseTimeout.Seconds()),
			Tool: &sdkcapability.ToolDecl{
				Description: spec.Description, Mode: sdkcapability.ToolModeBlocking,
				MaxAttempts: int32(binding.MaxAttempts),
			},
		})
	}
	declared := []sdkcapability.Capability{
		textgen.ManifestCapability(),
		websearch.ManifestCapability(),
		webfetch.ManifestCapability(),
		asr.ManifestCapability(),
		videogen.ManifestActivityCapability(),
	}
	declared = append(declared, tts.ManifestCapabilities()...)
	declared = append(declared, imagegen.ManifestCapabilities()...)
	declared = append(declared, mediaunderstand.ManifestCapabilities()...)
	for _, capability := range declared {
		def, err := capabilityDef(sdkcapability.CapabilityKindActivity, capability)
		if err != nil {
			return sdkcapability.ManifestV1{}, err
		}
		defs = append(defs, def)
	}
	for _, capability := range videogen.ManifestWorkflowCapabilities() {
		def, err := capabilityDef(sdkcapability.CapabilityKindWorkflow, capability)
		if err != nil {
			return sdkcapability.ManifestV1{}, err
		}
		defs = append(defs, def)
	}
	return (sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       coreTaskQueue,
		Capabilities:    defs,
	}).Normalize()
}

func capabilityDef(
	kind sdkcapability.CapabilityKind,
	declared sdkcapability.Capability,
) (sdkcapability.CapabilityDef, error) {
	def := sdkcapability.CapabilityDef{
		ID: declared.ID, Kind: kind, TemporalType: declared.TemporalType,
		Description:    declared.Description,
		InputSchema:    append(json.RawMessage(nil), declared.InputSchema...),
		OutputSchema:   append(json.RawMessage(nil), declared.OutputSchema...),
		TimeoutSeconds: declared.TimeoutSeconds,
	}
	if declared.Tool != nil {
		def.Tool = &sdkcapability.ToolDecl{
			Description: declared.Tool.Description, Mode: declared.Tool.Mode,
			MaxAttempts: declared.Tool.MaxAttempts,
		}
	}
	manifest, err := (sdkcapability.ManifestV1{
		ProtocolVersion: sdkcapability.ManifestProtocolVersion,
		TaskQueue:       coreTaskQueue,
		Capabilities:    []sdkcapability.CapabilityDef{def},
	}).Normalize()
	if err != nil {
		return sdkcapability.CapabilityDef{}, err
	}
	return manifest.Capabilities[0], nil
}
