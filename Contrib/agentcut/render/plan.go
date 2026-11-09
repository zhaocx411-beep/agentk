// Package render 把 timeline.Doc 编译成 ffmpeg-worker（FfmpegTool）的调用
// 序列，并提供执行该序列的 RenderTimelineWorkflow。
//
// 编译器（Compile）是纯函数：同一份 Doc 永远编译出同一个 Plan——它会在
// Temporal Workflow 内执行，必须确定性。MVP 语义（硬切、无转场）：
//
//   - 第一条 video 轨是底层画面：逐片段 clip_av/retime_av/freeze 预处理，
//     间隙补黑场，concat_av 拼成整条；
//   - 其余 video 轨按声明顺序整轨依次叠加：每个片段 overlay_region
//     （无几何信息，MVP 全画布覆盖）；
//   - audio 轨全部片段 mix_audio 一次混入（offsets_ms/gains 承载片段
//     位置与音量增益）；
//   - caption 轨编译成一份 SRT（base64 内联），最后 burn_subtitles。
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// FfmpegCapabilityID 是 ffmpeg-worker 发布的 Capability ID（见
// AgentK-Contrib/ffmpeg-worker/README.md）。通过 agentk-sdk 的
// capability.Invoke 按名字调用，不再需要硬编码 Task Queue / Workflow Type——
// 具体部署坐标由 agent-k 的路由服务动态解析，也因此不需要关心 agentcut
// worker 和 ffmpeg-worker 是否在同一个 kess division：只要两者都连的是
// agent-k 所在的 Temporal 集群，路由由 agent-k 侧统一解析。
const FfmpegCapabilityID = "contrib.ffmpeg_tool"

// Call 是一次 FfmpegTool Workflow 调用。
type Call struct {
	// ID 是计划内唯一、内容确定的标识，用于 artifact 引用与日志。
	ID string `json:"id"`
	// Input 是 FfmpegToolInput 的 JSON 形状（op/params 或 pipeline + 输入槽位）。
	// 槽位值可以是 OutputRef 占位符，执行时替换成前序调用的 artifact URI。
	Input map[string]any `json:"input"`
}

// Plan 是编译产物：按依赖拓扑排好序的调用序列，最后一个调用的 artifact
// 即渲染结果。
type Plan struct {
	Calls []Call `json:"calls"`
	// Warnings 是编译期发现的 MVP 限制（如逐片段音量、转场被忽略），
	// 不阻塞渲染，但要透出给用户/日志。
	Warnings []string `json:"warnings,omitempty"`
}

// refPrefix 是 artifact 占位符前缀。
const refPrefix = "$out:"

// OutputRef 返回引用某个 Call 产物的占位符。
func OutputRef(callID string) string { return refPrefix + callID }

const mediaRefPrefix = "$media:"

// MediaRef is an internal compiler reference. Local rendering resolves it only
// through the Timeline-registered BlobKey for this media ID.
func MediaRef(mediaID string) string { return mediaRefPrefix + mediaID }

func IsMediaRef(v string) (string, bool) {
	if strings.HasPrefix(v, mediaRefPrefix) {
		return v[len(mediaRefPrefix):], true
	}
	return "", false
}

// IsOutputRef 解析占位符，返回被引用的 callID。
func IsOutputRef(v string) (string, bool) {
	if strings.HasPrefix(v, refPrefix) {
		return v[len(refPrefix):], true
	}
	return "", false
}

// ResolveRefs 返回把 Input 中占位符替换为实际 URI 后的深拷贝。
// 未知引用返回 false（编译器有 bug 或调用方传了不完整的结果表）。
func ResolveRefs(input map[string]any, outputs map[string]string) (map[string]any, bool) {
	resolved, ok := resolveValue(input, outputs)
	if !ok {
		return nil, false
	}
	return resolved.(map[string]any), true
}

func resolveValue(v any, outputs map[string]string) (any, bool) {
	switch t := v.(type) {
	case string:
		if id, isRef := IsOutputRef(t); isRef {
			uri, ok := outputs[id]
			if !ok {
				return nil, false
			}
			return uri, true
		}
		return t, true
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			r, ok := resolveValue(s, outputs)
			if !ok {
				return nil, false
			}
			out[i] = r
		}
		return out, true
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			r, ok := resolveValue(e, outputs)
			if !ok {
				return nil, false
			}
			out[i] = r
		}
		return out, true
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			r, ok := resolveValue(e, outputs)
			if !ok {
				return nil, false
			}
			out[k] = r
		}
		return out, true
	default:
		return v, true
	}
}

// Key 由文档内容派生幂等渲染键：同一份文档（逐字节相同的 JSON 序列化）
// 必然得到同一个 key，重复导出不会重复生成大文件。
// Go 的 json.Marshal 对 struct 字段顺序是确定的，可直接作为规范化形式。
func Key(docJSON []byte) string {
	sum := sha256.Sum256(docJSON)
	return hex.EncodeToString(sum[:8])
}

// KeyOf 是 Key 的便捷封装。
func KeyOf(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return Key(b), nil
}

// RenderKey includes the immutable Timeline snapshot, render schema, executor,
// and profile so a behavioral change can never accidentally reuse old bytes.
func RenderKey(doc any, selection Selection) (string, error) {
	return KeyOf(struct {
		Schema   string       `json:"schema"`
		Executor ExecutorKind `json:"executor"`
		Profile  string       `json:"profile"`
		Doc      any          `json:"doc"`
	}{
		Schema: RenderSchemaVersion, Executor: selection.Executor,
		Profile: selection.Profile, Doc: doc,
	})
}
