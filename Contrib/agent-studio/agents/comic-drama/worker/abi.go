// 漫剧 Pipeline 的对外 ABI：workflow 类型名、任务队列、核心能力 Activity 名，
// 以及输入 / 输出（资产清单）的 JSON 形状。改动这里等于改协议，会断掉在飞执行。
//
// 阶段结构与 agentq drama-workflow（qai-sdk）对齐：全剧一次定妆 + 音色，
// 剧本按「第X集」切集，每集独立跑 资产描述 → 台词精简 → 分镜 → 资产图 →
// 首帧 → 视频 → 合成 的子流程（child workflow）。
package worker

import (
	"encoding/json"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	// CapabilityID 是发布到 agent-k Catalog 的能力 ID；profile-pipeline.json
	// 的 pipelines 声明必须指向它。格式必须是 contrib.<owner>.<name> 三段
	// （agent-k 校验），owner 与 call_agent 保持一致用 studio。
	CapabilityID = "contrib.studio.comic_drama_film"
	// PipelineName 是会话内的 pipeline 名，StartExecution 按它启动。
	PipelineName = "comic_drama_film"

	WorkflowType = "ComicDramaFilm"
	// EpisodeWorkflowType 是单集子流程的 workflow 类型名。只在父流程内部
	// 以 child workflow 启动，不发布到 Catalog。
	EpisodeWorkflowType  = "ComicDramaEpisode"
	WorkerTaskQueue      = "contrib.comic-drama-go"
	DefaultCoreTaskQueue = "agentk"

	generateTextActivity  = "agentk.capability.generate_text"
	generateImageActivity = "agentk.capability.generate_image"
	editImageActivity     = "agentk.capability.edit_image"
	// klingOmniVideoActivity 可灵 Omni 方言执行 activity。漫剧只走可灵，
	// 直调方言面以传入 referImages（通用 generate_video 最小面只收首帧，
	// 兑现不了 qai 的参考图锁定语义）。
	klingOmniVideoActivity = "agentk.capability.kling_omni_video_execute"
	concatVideosActivity   = "agentk.capability.concat_videos"
)

// DramaInput 是一次漫剧成片执行的全部输入。模型字段是 models catalog 中的
// capability ref，由 API Server 按部署环境填写；用户侧只提供 brief / title /
// stylePrompt / episodes。
type DramaInput struct {
	// Brief 题材、梗概或完整多集剧本（含「第X集」标记时按标记切集）。
	Brief string `json:"brief"`
	// Title 可选作品名；空则由剧本阶段生成。
	Title string `json:"title,omitempty"`
	// StylePrompt 全剧统一视觉风格锚点；空则使用默认日漫赛璐珞风格。
	StylePrompt string `json:"stylePrompt,omitempty"`
	// Episodes 目标集数，默认 1，上限 10（qai drama-script 同限）。用户给
	// 完整剧本时以切集结果为准，仍受上限截断。
	Episodes int `json:"episodes,omitempty"`
	// MaxShots 每集分镜数上限，默认与上限均为 10（qai MAX_SHOTS_PER_EPISODE）。
	MaxShots int `json:"maxShots,omitempty"`

	ChatModel  string `json:"chatModel"`
	ImageModel string `json:"imageModel"`
	EditModel  string `json:"editModel"`
	VideoModel string `json:"videoModel"`

	// AspectRatio 成片画幅，默认 9:16。Resolution 默认 1080p。Audio 默认 native。
	AspectRatio string `json:"aspectRatio,omitempty"`
	Resolution  string `json:"resolution,omitempty"`
	Audio       string `json:"audio,omitempty"`
}

// DramaOutput 是完整资产清单：不只成片 URL，人物定妆与音色、逐集资产图、
// 每镜首帧与片段、各阶段实际使用的 prompt 全部返回，供下游存档或修改使用。
type DramaOutput struct {
	Title       string `json:"title"`
	Synopsis    string `json:"synopsis,omitempty"`
	Script      string `json:"script"`
	StyleAnchor string `json:"styleAnchor"`
	// Characters 全剧人物定妆三视图（qai costume_desc + costume images）。
	Characters []CharacterAsset `json:"characters"`
	// Voices 全剧固定音色表（含旁白），逐镜注入视频 prompt。
	Voices []VoiceAsset `json:"voices,omitempty"`
	// Episodes 逐集产物（时间序）。单集剧也走同一结构。
	Episodes []EpisodeAsset `json:"episodes"`
	// Film 全剧成片：单集时即该集成片；多集时按集序把各集成片再合成。
	Film string `json:"film"`
	// ClipCount 全剧成功镜头总数。
	ClipCount int `json:"clipCount"`
}

type CharacterAsset struct {
	Name string `json:"name"`
	// Variant 长期造型变化的变体标注（qai costume.variant），通常为空。
	Variant string `json:"variant,omitempty"`
	// Prompt 定妆照实际使用的完整生图 prompt。
	Prompt   string `json:"prompt"`
	ImageURL string `json:"imageUrl,omitempty"`
	Error    string `json:"error,omitempty"`
}

// VoiceAsset 一个角色（或旁白）的全剧固定音色（qai voice_desc）。
type VoiceAsset struct {
	Name   string `json:"name"`
	Gender string `json:"gender,omitempty"`
	Age    string `json:"age,omitempty"`
	Timbre string `json:"timbre"`
}

// EpisodeAsset 一集的全部产物。Error 非空表示该集整体失败（其余集不受影响）。
type EpisodeAsset struct {
	// Number 集号，从 1 开始。
	Number int    `json:"number"`
	Title  string `json:"title,omitempty"`
	// Script 本集剧本（台词精简后的实际生产版本）。
	Script string `json:"script"`
	// Assets 本集图片资产（人物状态 / 场景 / 道具）。
	Assets []EpisodeImageAsset `json:"assets,omitempty"`
	Shots  []ShotAsset         `json:"shots"`
	// Film 本集成片。
	Film      string `json:"film,omitempty"`
	ClipCount int    `json:"clipCount"`
	Error     string `json:"error,omitempty"`
}

// EpisodeImageAsset 一条分集图片资产（qai AssetEntry）。
type EpisodeImageAsset struct {
	// Category 人物状态 / 场景 / 道具。
	Category string `json:"category"`
	Name     string `json:"name"`
	// CharacterRef 人物状态图对应的角色基本名；其他类别为空。
	CharacterRef string `json:"characterRef,omitempty"`
	// Prompt 实际使用的完整生图 prompt；复用前集资产时为空。
	Prompt string `json:"prompt,omitempty"`
	// ReuseFromEp >0 表示复用该集的同名资产。
	ReuseFromEp int    `json:"reuseFromEp,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ShotAsset 一镜的分镜结构（qai StoryboardShot）与产物。
type ShotAsset struct {
	Index int `json:"index"`
	// ShotID 两位数字串（"01"…），与分镜脚本一致。
	ShotID     string   `json:"shotId"`
	Characters []string `json:"characters,omitempty"`
	Scenes     []string `json:"scenes,omitempty"`
	Props      []string `json:"props,omitempty"`
	// Speakers 本镜说话人（按首次开口顺序，旁白写"旁白"）。
	Speakers []string `json:"speakers,omitempty"`
	// FirstFrame 起始静帧描述；Storyboard 完整分镜正文（含镜头标注与台词）。
	FirstFrame string `json:"firstFrame"`
	Storyboard string `json:"storyboard"`
	// FramePrompt / VideoPrompt 实际派发给模型的完整 prompt。
	FramePrompt     string `json:"framePrompt"`
	VideoPrompt     string `json:"videoPrompt"`
	DurationSeconds int    `json:"durationSeconds"`
	FrameURL        string `json:"frameUrl,omitempty"`
	VideoURL        string `json:"videoUrl,omitempty"`
	Error           string `json:"error,omitempty"`
}

/* ---------- 单集子流程（child workflow）的输入 ---------- */

// EpisodeContext 前集上下文：资产描述看前两集（复用判断），分镜看前一集
// （连续性），与 qai steps/asset-desc.ts、steps/storyboard.ts 的取材一致。
type EpisodeContext struct {
	Number int                 `json:"number"`
	Script string              `json:"script"`
	Assets []EpisodeImageAsset `json:"assets,omitempty"`
	Shots  []ShotAsset         `json:"shots,omitempty"`
}

// EpisodeInput 单集子流程的输入，父流程组装。
type EpisodeInput struct {
	Number      int    `json:"number"`
	Title       string `json:"title,omitempty"`
	Script      string `json:"script"`
	StyleAnchor string `json:"styleAnchor"`
	MaxShots    int    `json:"maxShots"`
	// Characters 全剧定妆三视图（含 URL），资产图与首帧的身份参考。
	Characters []CharacterAsset `json:"characters"`
	// Voices 全剧固定音色表，逐镜注入视频 prompt。
	Voices []VoiceAsset `json:"voices,omitempty"`
	// Prev 前集上下文，时间倒序（Prev[0] 是上一集），最多两集。
	Prev []EpisodeContext `json:"prev,omitempty"`

	ChatModel   string `json:"chatModel"`
	ImageModel  string `json:"imageModel"`
	EditModel   string `json:"editModel"`
	VideoModel  string `json:"videoModel"`
	AspectRatio string `json:"aspectRatio"`
	Resolution  string `json:"resolution"`
	Audio       string `json:"audio"`
}

/* ---------- 各阶段模型输出的 JSON 形状（qai snake_case） ---------- */

type scriptResult struct {
	Title    string `json:"title"`
	Synopsis string `json:"synopsis"`
	Script   string `json:"script"`
}

type costumeResult struct {
	Costumes []costumeDesc `json:"costumes"`
	Voices   []VoiceAsset  `json:"voices"`
}

type costumeDesc struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
	Prompt  string `json:"prompt"`
}

type splitResult struct {
	Episodes []splitEpisode `json:"episodes"`
}

type splitEpisode struct {
	Ep      int    `json:"ep"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type assetDescResult struct {
	Assets []assetDesc `json:"assets"`
}

type assetDesc struct {
	Category     string `json:"category"`
	Name         string `json:"name"`
	CharacterRef string `json:"character_ref"`
	Prompt       string `json:"prompt"`
	ReuseFromEp  *int   `json:"reuse_from_ep"`
	// ReuseAssetName 复用时前集资产的准确名称（qai 用结构化 id，这里按名字匹配）。
	ReuseAssetName string `json:"reuse_asset_name"`
}

type simplifyResult struct {
	Script string `json:"script"`
}

type storyboardResult struct {
	Shots []storyboardShot `json:"shots"`
}

type storyboardShot struct {
	ShotID     string   `json:"shot_id"`
	DurationS  int      `json:"duration_s"`
	Characters []string `json:"characters"`
	Scenes     []string `json:"scenes"`
	Props      []string `json:"props"`
	FirstFrame string   `json:"first_frame"`
	Storyboard string   `json:"storyboard"`
	Speakers   []string `json:"speakers"`
}

/* ---------- 核心能力 Activity 的载荷（与 agent-k 公开 ABI 同形） ---------- */

type generateTextInput struct {
	Model        string          `json:"model"`
	Prompt       string          `json:"prompt"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type generateTextOutput struct {
	Result json.RawMessage `json:"result"`
}

type generateImageInput struct {
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspectRatio,omitempty"`
}

type generateImageOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
}

type editImageInput struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	ReferenceImages []string `json:"referenceImages"`
	AspectRatio     string   `json:"aspectRatio,omitempty"`
}

type editImageOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
}

// klingOmniVideoInput 与 agent-k KlingOmniInput（model + KlingOmniRequest
// 摊平）同形，只声明漫剧用到的字段。Model 必须是可灵 Omni 方言条目。
type klingOmniVideoInput struct {
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	FirstFrame string `json:"firstFrame"`
	// ReferImages 元素参考图（人物状态/定妆、场景、道具），编号
	// image_1..image_N，prompt 内用 @image_n 指代；上限 7 张。
	ReferImages []string `json:"referImages,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Duration    string   `json:"duration,omitempty"`
	// Audio native 生成原生台词声音；画幅跟随首帧，不传 aspectRatio。
	Audio string `json:"audio,omitempty"`
}

type klingOmniVideoOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
}

type concatVideosInput struct {
	VideoURLs []string `json:"videoUrls"`
}

type concatVideosOutput struct {
	Artifacts []sdkcapability.Artifact `json:"artifacts"`
	ClipCount int                      `json:"clipCount"`
}

// firstArtifactURI 取媒体能力输出的首个 artifact URI；没有产物返回空串。
func firstArtifactURI(artifacts []sdkcapability.Artifact) string {
	if len(artifacts) == 0 {
		return ""
	}
	return artifacts[0].URI
}
