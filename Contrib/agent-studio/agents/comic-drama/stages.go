// Agent 模式的画布投影：把自由会话折叠出的产物与在途调用，映射到
// 漫剧固定工序的五个阶段上（剧本 → 定妆/场景 → 首帧 → 镜头 → 成片）。
// 仅用于展示：归类凭产出工具名 + 产物描述启发式（edit_image 既产
// 场景/道具图也产分镜首帧，靠描述区分）。
package comicdrama

import (
	"strings"
	"time"
)

var agentStageDefs = []struct{ ID, Name string }{
	{"script", "剧本与设定"},
	{"characters", "定妆 · 场景 · 道具"},
	{"frames", "分镜首帧"},
	{"clips", "镜头视频"},
	{"film", "成片合成"},
}

type stageDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Status pending / active / done。active 优先于 done：分镜逐个
	// 生成时既有产物也有在途调用，画布应显示"进行中"。
	Status string     `json:"status"`
	Assets []assetDTO `json:"assets,omitempty"`
}

type assetDTO struct {
	Kind string `json:"kind"`
	URI  string `json:"uri"`
	Tool string `json:"tool,omitempty"`
	// Stage 画布列归属（characters/frames/clips/film）。
	Stage       string    `json:"stage,omitempty"`
	Description string    `json:"description,omitempty"`
	At          time.Time `json:"at,omitempty"`
}

// edgeDTO 一条生成血缘：from 资产参与生成了 to 资产。
type edgeDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// pendingDTO 一次在途生成：画布上显示为"生成中"占位节点，
// Inputs 里的已知资产会连流动的线过去。
type pendingDTO struct {
	ID     string   `json:"id"`
	Stage  string   `json:"stage"`
	Tool   string   `json:"tool,omitempty"`
	Inputs []string `json:"inputs,omitempty"`
}

type messageDTO struct {
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at,omitempty"`
}

// stageForTool 工具名 → 阶段。空串表示不属于任何画布阶段。
func stageForTool(tool string) string {
	switch tool {
	case "generate_image":
		return "characters"
	case "edit_image":
		return "frames"
	case "kling_omni_video", "kling_text_to_video", "kling_image_to_video":
		return "clips"
	}
	if _, ok := composeToolNames[tool]; ok {
		return "film"
	}
	return ""
}

// stageForAsset 归类一条产物。edit_image 既产场景/道具/状态图（参考资产，
// 归定妆列）也产分镜首帧，靠模型写的 description 区分；写明"首帧"的
// 优先归首帧。工具未知时按类型兜底（图 → 首帧，视频 → 镜头）。
func stageForAsset(asset DramaAsset) string {
	if asset.Tool == "edit_image" {
		description := asset.Description
		if strings.Contains(description, "首帧") {
			return "frames"
		}
		for _, marker := range []string{"场景", "道具", "定妆", "状态图", "三视图"} {
			if strings.Contains(description, marker) {
				return "characters"
			}
		}
		return "frames"
	}
	if stage := stageForTool(asset.Tool); stage != "" {
		return stage
	}
	switch asset.Kind {
	case "image":
		return "frames"
	case "video":
		return "clips"
	}
	return ""
}

func bucketHasURI(assets []assetDTO, uri string) bool {
	for _, asset := range assets {
		if asset.URI == uri {
			return true
		}
	}
	return false
}

// agentPending 在途的生成调用/任务 → 画布占位节点。
func agentPending(state DramaState) []pendingDTO {
	appendPending := func(list []pendingDTO, callID string) []pendingDTO {
		tool := state.ToolNames[callID]
		stage := stageForTool(tool)
		if stage == "" || stage == "script" {
			return list
		}
		// 只保留已登记为资产的输入：外部 URL 在画布上没有源节点。
		var inputs []string
		for _, input := range state.CallInputs[callID] {
			if _, known := state.SeenAssets[input]; known {
				inputs = append(inputs, input)
			}
		}
		return append(list, pendingDTO{ID: callID, Stage: stage, Tool: tool, Inputs: inputs})
	}

	var pending []pendingDTO
	seen := make(map[string]struct{})
	for callID := range state.OpenCalls {
		if _, dup := seen[callID]; dup {
			continue
		}
		seen[callID] = struct{}{}
		pending = appendPending(pending, callID)
	}
	for jobID := range state.OpenJobs {
		callID := state.JobCalls[jobID]
		if callID == "" {
			continue
		}
		if _, dup := seen[callID]; dup {
			continue
		}
		seen[callID] = struct{}{}
		pending = appendPending(pending, callID)
	}
	return pending
}

// agentStages 计算画布五阶段的状态与各自的产物。
func agentStages(state DramaState, status DramaStatus) []stageDTO {
	buckets := make(map[string][]assetDTO)
	for _, asset := range state.Assets {
		stage := stageForAsset(asset)
		if stage == "" || stage == "film" {
			// 成片以 Films 为准（旧契约的 result.videoUrl 不在 Assets 里）。
			continue
		}
		buckets[stage] = append(buckets[stage], assetDTO{
			Kind: asset.Kind, URI: asset.URI, Tool: asset.Tool,
			Stage: stage, Description: asset.Description, At: asset.At,
		})
	}
	for _, url := range state.Films {
		if bucketHasURI(buckets["film"], url) {
			continue
		}
		buckets["film"] = append(buckets["film"], assetDTO{
			Kind: "video", URI: url, Stage: "film", Tool: "compose_video",
		})
	}

	// 在途调用与异步任务 → 阶段"进行中"。
	open := make(map[string]bool)
	for callID := range state.OpenCalls {
		if stage := stageForTool(state.ToolNames[callID]); stage != "" {
			open[stage] = true
		}
	}
	for jobID := range state.OpenJobs {
		if stage := stageForTool(state.ToolNames[state.JobCalls[jobID]]); stage != "" {
			open[stage] = true
		}
	}

	assistantSpoke := false
	for _, message := range state.Messages {
		if message.Role == "assistant" {
			assistantSpoke = true
			break
		}
	}
	running := status == DramaRunning

	stages := make([]stageDTO, 0, len(agentStageDefs))
	activeSeen := false
	for _, def := range agentStageDefs {
		var done bool
		switch def.ID {
		case "script":
			// 剧本没有独立产物：模型开始产出正文或任何素材即算完成。
			done = assistantSpoke || len(state.Assets) > 0 || len(state.Films) > 0
		case "film":
			done = len(state.Films) > 0
		default:
			done = len(buckets[def.ID]) > 0
		}
		stageStatus := "pending"
		switch {
		case running && open[def.ID]:
			stageStatus = "active"
			activeSeen = true
		case done:
			stageStatus = "done"
		}
		stages = append(stages, stageDTO{
			ID: def.ID, Name: def.Name, Status: stageStatus, Assets: buckets[def.ID],
		})
	}
	// 轮与轮之间没有在途调用：第一个未完成阶段视为进行中。
	if running && !activeSeen {
		for index := range stages {
			if stages[index].Status == "pending" {
				stages[index].Status = "active"
				break
			}
		}
	}
	return stages
}
