package worker

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// manifestCollector 累计 Worker 启动前声明的 capability。启动时构建并冻结
// Manifest，之后再声明即为使用错误。
type manifestCollector struct {
	mu          sync.Mutex
	definitions []capability.CapabilityDef
	frozen      bool
	lookup      capabilityLookup
}

// isCapability 报告 temporalType 是否是已冻结 Manifest 中的 capability；
// 冻结前一律为 false。
func (c *manifestCollector) isCapability(kind capability.CapabilityKind, temporalType string) bool {
	c.mu.Lock()
	lookup := c.lookup
	c.mu.Unlock()
	return lookup != nil && lookup(kind, temporalType)
}

func newManifestCollector() *manifestCollector {
	return &manifestCollector{}
}

func (c *manifestCollector) add(kind capability.CapabilityKind, declared capability.Capability) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mustBeMutable()

	def := capability.CapabilityDef{
		ID:             declared.ID,
		Kind:           kind,
		TemporalType:   declared.TemporalType,
		Description:    declared.Description,
		InputSchema:    cloneRawMessage(declared.InputSchema),
		OutputSchema:   cloneRawMessage(declared.OutputSchema),
		TimeoutSeconds: declared.TimeoutSeconds,
	}
	if declared.Tool != nil {
		def.Tool = &capability.ToolDecl{
			Description: declared.Tool.Description,
			Mode:        capability.ToolModeBlocking,
			MaxAttempts: declared.Tool.MaxAttempts,
		}
	}
	c.definitions = append(c.definitions, def)
}

func (c *manifestCollector) buildAndFreeze(
	taskQueue string,
	lane string,
) (capability.ManifestV1, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	manifest, err := (capability.ManifestV1{
		ProtocolVersion: capability.ManifestProtocolVersion,
		TaskQueue:       taskQueue,
		LaneID:          lane,
		Capabilities:    c.definitions,
	}).Normalize()
	if err != nil {
		return capability.ManifestV1{}, err
	}
	if taskQueue == coreTaskQueue {
		return capability.ManifestV1{}, fmt.Errorf("contributor worker task queue must not be %q", coreTaskQueue)
	}
	for _, def := range manifest.Capabilities {
		if !strings.HasPrefix(def.ID, "contrib.") {
			return capability.ManifestV1{}, fmt.Errorf(
				"contributor worker capability id %q must match contrib.<owner>.<name>",
				def.ID,
			)
		}
	}
	c.frozen = true
	c.lookup = manifestLookup(manifest)
	return manifest, nil
}

func (c *manifestCollector) mustBeMutable() {
	if c.frozen {
		panic("agentk worker: capability manifest is frozen after worker start")
	}
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}
