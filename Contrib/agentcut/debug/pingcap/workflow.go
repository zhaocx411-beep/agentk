// Package pingcap 提供一个仅用于联调的通用 Workflow：按 capability ID
// 转发一次 capability.Invoke 调用并回传原始 JSON 结果。
//
// 用途：验证 agentcut worker 到 agent-k 路由/Temporal 集群这条链路本身是否
// 打通，与具体目标 capability（如 contrib.ffmpeg_tool）是否已上线是两件
// 是独立的事——用一个保证一直在线的内置能力（如 agentk.web_search）做目标，
// 就能把"我们的调用代码/连接对不对"和"对方 worker 在不在线"分开验证。
//
// 不发布为 capability（不经 w.PublishActivity/PublishWorkflow），只注册在
// agentcut 自己的 Task Queue 上，由 cmd/pingcapability 直接按 WorkflowName
// 启动执行，agent-k 侧感知不到它的存在。
package pingcap

import (
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/workflow"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

// WorkflowName 是本 Workflow 的 Temporal 注册名。
const WorkflowName = "agentcut.debug.PingCapability"

// Input 指定要调用的 capability 及其入参（透传给 capability.Invoke）。
type Input struct {
	// CapabilityID 例如 "agentk.web_search" 或 "contrib.ffmpeg_tool"。
	CapabilityID string `json:"capabilityId"`
	// Input 是目标 capability 的入参，原样反序列化后传给 capability.Invoke；
	// 用 json.RawMessage 而不是具体类型，这样本工具能测任意 capability。
	Input json.RawMessage `json:"input"`
}

// Output 携带目标 capability 的原始返回。
type Output struct {
	Output json.RawMessage `json:"output"`
}

// Workflow 解析 Input.Input 为 map 后转发一次 capability.Invoke，
// 再把结果重新编码为 JSON 带回——这样调用方（cmd/pingcapability）不需要
// 知道目标 capability 的具体 Go 类型。
func Workflow(ctx workflow.Context, in Input) (Output, error) {
	if in.CapabilityID == "" {
		return Output{}, fmt.Errorf("pingcap: capabilityId is required")
	}

	var input map[string]any
	if len(in.Input) > 0 {
		if err := json.Unmarshal(in.Input, &input); err != nil {
			return Output{}, fmt.Errorf("pingcap: invalid input json: %w", err)
		}
	}

	var output map[string]any
	if err := sdkcapability.Invoke(ctx, in.CapabilityID, input, &output); err != nil {
		return Output{}, fmt.Errorf("pingcap: invoke %s: %w", in.CapabilityID, err)
	}

	raw, err := json.Marshal(output)
	if err != nil {
		return Output{}, fmt.Errorf("pingcap: marshal output: %w", err)
	}
	return Output{Output: raw}, nil
}
