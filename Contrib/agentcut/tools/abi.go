// Package tools 实现暴露给 agent-k 的 contrib.agentcut.* 工具。
//
// 每个工具是一个薄 Temporal Activity：校验 op 白名单后，把请求转发成对目标
// TimelineEntityWorkflow 的一次 Update（timeline_get 则是 Query）。工具自身
// 不含任何领域逻辑；source 固定为 agent，模型无法伪装成 user/system。
//
// projectId 仍作为工具参数传入；媒体登记会另外从 Activity 的父 Session
// Workflow 读取 sessionId，并与 Timeline 的服务端绑定值比对，不能只信任
// 可被模型显式参数覆盖的 toolDefaults。
package tools

import (
	"agentcut/timeline"
)

const (
	// InternalTaskQueue 承载 Timeline、render、prepare 等 AgentCut 私有
	// Workflow/Activity。它固定由 baseline Worker 消费。
	InternalTaskQueue = "contrib.agentcut"

	// WorkerTaskQueue 是发布给 Agent-K 的 capability 逻辑基础队列。Agent-K
	// Worker SDK 会让泳道实例实际监听 WorkerTaskQueue+"@"+lane。
	WorkerTaskQueue = "contrib.agentcut.capability"
)

// Capability ID（三段式，对齐 agent-k 最新校验规则）与 Activity 类型名。
const (
	CapTimelineGet            = "contrib.agentcut.timeline_get"
	CapTimelineEditClips      = "contrib.agentcut.timeline_edit_clips"
	CapTimelineEditTracks     = "contrib.agentcut.timeline_edit_tracks"
	CapTimelineSetCaptions    = "contrib.agentcut.timeline_set_captions"
	CapTimelineEditMedia      = "contrib.agentcut.timeline_edit_media"
	CapTimelineEditTranscript = "contrib.agentcut.timeline_edit_transcript"
)

// op 白名单：哪个工具允许提交哪些 op。媒体登记类 op（addMedia/removeMedia/
// setTranscript）只走系统上传链路，不暴露给模型。
var (
	editClipsAllowed = map[string]bool{
		timeline.OpAddItem:          true,
		timeline.OpRemoveItems:      true,
		timeline.OpSplit:            true,
		timeline.OpTrimStart:        true,
		timeline.OpTrimEnd:          true,
		timeline.OpMoveItem:         true,
		timeline.OpUpdateItem:       true,
		timeline.OpRippleDelete:     true,
		timeline.OpAddTransition:    true,
		timeline.OpUpdateTransition: true,
		timeline.OpRemoveTransition: true,
	}
	editTracksAllowed = map[string]bool{
		timeline.OpAddTrack:    true,
		timeline.OpRemoveTrack: true,
		timeline.OpUpdateTrack: true,
		timeline.OpSetMetadata: true,
	}
	setCaptionsAllowed = map[string]bool{
		timeline.OpSetCaptions: true,
	}
	// timeline_edit_media 允许模型把 AIGC 产物（generate_image / kling 生成的
	// artifact URL）登记为素材、修改素材展示名，或移除未被引用的素材。
	// 用户上传的素材登记走系统上传链路，不经过这个工具。
	editMediaAllowed = map[string]bool{
		timeline.OpAddMedia:    true,
		timeline.OpRemoveMedia: true,
		timeline.OpUpdateMedia: true,
	}
)

// GetInput / GetOutput 是 timeline_get 的契约。
type GetInput struct {
	ProjectID string `json:"projectId" jsonschema:"minLength=1,description=项目 ID"`
}

type GetOutput struct {
	Revision         uint64       `json:"revision" jsonschema:"description=当前文档版本号；提交修改时必须把它作为 baseRevision 传回"`
	DurationInFrames int64        `json:"durationInFrames" jsonschema:"description=时间线总长（帧）"`
	Doc              timeline.Doc `json:"doc" jsonschema:"description=完整 Timeline 文档"`
}

// EditInput 是 timeline_edit_clips / timeline_edit_tracks 的公共契约。
type EditInput struct {
	ProjectID    string               `json:"projectId" jsonschema:"minLength=1,description=项目 ID"`
	BaseRevision uint64               `json:"baseRevision" jsonschema:"description=编辑基于的文档版本号；必须来自最近一次 timeline_get 或上次编辑返回的 revision"`
	Operations   []timeline.Operation `json:"operations" jsonschema:"description=要执行的操作批次，任一失败整批不生效"`
}

// SetCaptionsInput 是 timeline_set_captions 的契约（整轨替换）。
type SetCaptionsInput struct {
	ProjectID    string             `json:"projectId" jsonschema:"minLength=1,description=项目 ID"`
	BaseRevision uint64             `json:"baseRevision" jsonschema:"description=编辑基于的文档版本号"`
	TrackID      string             `json:"trackId" jsonschema:"minLength=1,description=字幕轨 ID（kind 必须是 caption）"`
	Captions     []timeline.Caption `json:"captions" jsonschema:"description=整轨字幕内容；传空数组清空该轨"`
}

// EditOutput 是所有修改类工具的统一返回。领域内的预期失败（版本冲突、
// op 校验失败）以 ok=false 返回而不是 Activity 错误，模型看到结构化原因
// 后可以自行纠正重试，避免无意义的 Activity 重试。
type EditOutput struct {
	OK       bool                    `json:"ok"`
	Revision uint64                  `json:"revision,omitempty" jsonschema:"description=修改成功后的新版本号"`
	Summary  *timeline.ChangeSummary `json:"summary,omitempty"`
	// 失败时的机器可读原因。
	Code  string `json:"code,omitempty" jsonschema:"description=失败码：REVISION_CONFLICT 表示 baseRevision 过期，需要重新 timeline_get；其余为 op 校验失败码"`
	Error string `json:"error,omitempty"`
	// REVISION_CONFLICT 时返回最新版本号，可直接用它重试。
	LatestRevision        uint64 `json:"latestRevision,omitempty"`
	addedMediaGenerations map[string]uint64
}

type TranscriptEditInput struct {
	ProjectID        string                           `json:"projectId" jsonschema:"minLength=1,description=项目 ID"`
	Action           string                           `json:"action" jsonschema:"enum=view,enum=delete,description=view 获取权威项目文字稿；delete 按 occurrenceId 原子删字剪视频"`
	BaseRevision     uint64                           `json:"baseRevision,omitempty" jsonschema:"description=delete 必填，必须来自最近一次 view"`
	Ranges           []timeline.TranscriptDeleteRange `json:"ranges,omitempty" jsonschema:"description=delete 的文字稿选择范围；不得提交帧坐标"`
	Mode             string                           `json:"mode,omitempty" jsonschema:"enum=ripple,description=delete 仅支持 ripple"`
	FromOccurrenceID string                           `json:"fromOccurrenceId,omitempty" jsonschema:"description=view 可选的起始 occurrenceId"`
	ToOccurrenceID   string                           `json:"toOccurrenceId,omitempty" jsonschema:"description=view 可选的结束 occurrenceId"`
	Limit            int                              `json:"limit,omitempty" jsonschema:"minimum=1,maximum=500,description=view 最多返回的条目数"`
}

type TranscriptEditOutput struct {
	OK                      bool                            `json:"ok"`
	Action                  string                          `json:"action"`
	TimelineRevision        uint64                          `json:"timelineRevision"`
	Entries                 []timeline.TranscriptEntry      `json:"entries,omitempty"`
	Total                   int                             `json:"total,omitempty"`
	Truncated               bool                            `json:"truncated,omitempty"`
	Warnings                []string                        `json:"warnings,omitempty"`
	Revision                uint64                          `json:"revision,omitempty"`
	Doc                     *timeline.Doc                   `json:"doc,omitempty"`
	Summary                 *timeline.ChangeSummary         `json:"summary,omitempty"`
	DeletedRanges           []timeline.TranscriptFrameRange `json:"deletedRanges,omitempty"`
	DeletedDurationInFrames int64                           `json:"deletedDurationInFrames,omitempty"`
	Code                    string                          `json:"code,omitempty"`
	Error                   string                          `json:"error,omitempty"`
	LatestRevision          uint64                          `json:"latestRevision,omitempty"`
}

const (
	descGet = "读取项目当前的完整 Timeline 文档与版本号。任何修改前都应先调用它拿到最新 revision。"

	descEditClips = "编辑时间线上的片段：addItem 添加片段、removeItems 删除、split 分割、" +
		"trimStart/trimEnd 调整边缘（绝对帧）、moveItem 移动/跨轨、updateItem 改速率/音量/静音/淡入淡出/裁剪/旋转/画面区域/文字/复杂字幕样式、" +
		"rippleDelete 删除时间区间并把后续内容左移，addTransition/updateTransition/removeTransition 管理相邻视频片段的 crossfade 或 dipToBlack 转场。" +
		"所有帧坐标使用文档 metadata.fps 的帧空间。" +
		"必须携带最近一次读取到的 baseRevision。"

	descEditTracks = "编辑轨道与画布：addTrack/removeTrack/updateTrack（锁定轨只允许改 locked 字段）、" +
		"updateTrack 可配置音轨 ducking（sidechainTrackIds/gain/attackFrames/releaseFrames）；视频轨 hidden 只隐藏画面并保留内嵌音频，muted 只控制声音，" +
		"setMetadata 修改画布尺寸等。必须携带 baseRevision。"

	descSetCaptions = "整轨替换字幕轨内容（先清空再写入给定字幕列表）。适合把 ASR 转写结果或翻译后的" +
		"字幕一次性写入；每条字幕可用 captionStyle 按字段覆盖字体、颜色、描边、位置和排版。必须携带 baseRevision。"

	descEditMedia = "登记、重命名或移除素材：addMedia 把生成工具产出的 artifact URL 登记为可用素材" +
		"（必须使用真实的工具返回 URL，durationInFrames 按文档 fps 换算，图片传 0）、" +
		"updateMedia 仅允许通过 mediaId 和 mediaPatch.displayName 修改素材展示名（最长 256 字符）、" +
		"removeMedia 移除未被引用的素材。必须携带 baseRevision。"

	descEditTranscript = "读取按当前时间线播放顺序派生的项目文字稿，或根据服务端 occurrenceId 范围执行删字剪视频。" +
		"必须先 action=view 获取 occurrenceId 和 timelineRevision，再用 action=delete、相同 baseRevision、mode=ripple 提交范围。" +
		"禁止自行计算或提交 fromFrame/toFrame；冲突或选择失效后必须重新 view。"
)
