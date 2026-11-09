package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListArtifactsToolName 内置产物清单工具名。
const ListArtifactsToolName = "list_artifacts"

// defaultArtifactLimit 在可供模型选择的资源数量和上下文占用之间取固定上限。
const defaultArtifactLimit = 20

// NewListArtifactsTool 构造读取 Session 资源事件投影的工具。
// 它用于找回已退出上下文窗口或异步完成的资源 URL，仅访问本地投影，不提供网络能力。
func NewListArtifactsTool() Spec {
	return Spec{
		Name: ListArtifactsToolName,
		Description: "列出本会话已生成的图片、视频与音频（包括早先轮次的产物）。" +
			"每条含自然语言 description、url、产出它的工具与模型、以及当时的完整入参（prompt 等）。" +
			"url 可直接作为下游工具的入参：生视频工具的首帧图或参考素材、edit_image 的参考图、" +
			"process_video/compose_video 的视频、compose_video 的音频。" +
			"需要引用之前生成的素材、而上下文里已看不到它的 URL 时用这个工具，不要凭印象编造 URL；" +
			"用户要求「照着那张再来一版」时，也从这里取回原始入参再改。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"type": {"type": "string", "enum": ["image", "video", "audio"], "description": "只看某一类产物；缺省返回全部媒体产物"},
				"limit": {"type": "integer", "minimum": 1, "description": "最多返回几条（取最近的）；缺省 20"}
			}
		}`),
		Binding: InlineBinding{Execute: func(_ context.Context, input json.RawMessage, tc Context) (any, error) {
			if tc.ListArtifacts == nil {
				return nil, fmt.Errorf("list_artifacts: artifact registry not available in this runtime")
			}
			var in struct {
				Type  string `json:"type"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return nil, err
			}
			switch in.Type {
			case "", "image", "video", "audio":
			default:
				return nil, fmt.Errorf("list_artifacts: unknown type %q (want image, video, or audio)", in.Type)
			}
			all, err := tc.ListArtifacts()
			if err != nil {
				return nil, err
			}
			artifacts := make([]ArtifactInfo, 0, len(all))
			for _, a := range all {
				if in.Type == "" || a.Type == in.Type {
					artifacts = append(artifacts, a)
				}
			}
			limit := in.Limit
			if limit <= 0 {
				limit = defaultArtifactLimit
			}
			// 投影按时间升序，因此从尾部取最近资源。
			truncated := len(artifacts) > limit
			if truncated {
				artifacts = artifacts[len(artifacts)-limit:]
			}
			out := map[string]any{"artifacts": artifacts}
			switch {
			// 区分 Session 无资源和 type 过滤结果为空。
			case len(artifacts) == 0 && in.Type != "":
				out["note"] = fmt.Sprintf("本会话没有 %s 类产物（其他类型可能有，去掉 type 再查）。", in.Type)
			case len(artifacts) == 0:
				out["note"] = "本会话还没有已登记的产物。"
			case truncated:
				out["note"] = fmt.Sprintf("只列出了最近 %d 条；更早的产物需要调大 limit。", limit)
			}
			return out, nil
		}},
	}
}
