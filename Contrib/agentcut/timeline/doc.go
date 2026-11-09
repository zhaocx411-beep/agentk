// Package timeline 是 AgentCut 的纯领域内核：Timeline 文档类型、Ops 协议与
// ApplyOps 纯函数。本包不依赖 Temporal、HTTP、BlobStore 或任何存储；
// 所有函数必须是确定性的（它们会在 Temporal Workflow 内执行）。
package timeline

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// SchemaVersion 是当前唯一接受的文档版本。
const SchemaVersion = 1

// Doc 是一条 Timeline 的完整文档。权威状态保存在 TimelineEntityWorkflow 中，
// 本结构只描述数据形状。
type Doc struct {
	SchemaVersion int          `json:"schemaVersion"`
	Metadata      Metadata     `json:"metadata"`
	Media         []Media      `json:"media"`
	Tracks        []Track      `json:"tracks"`
	Items         []Item       `json:"items"`
	Transitions   []Transition `json:"transitions,omitempty"`
}

// Metadata 是画布与全局属性。FPS 是整份文档的标准帧空间：
// 所有 from/durationInFrames/trimStart 都按此 fps 表达。
type Metadata struct {
	Name            string `json:"name,omitempty"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	FPS             int    `json:"fps"`
	BackgroundColor string `json:"backgroundColor,omitempty"`
}

// ValidateBackgroundColor keeps Timeline validation and render compilation on
// the same deterministic, injection-safe color subset.
func ValidateBackgroundColor(value string) error {
	if value == "" {
		return nil
	}
	switch strings.ToLower(value) {
	case "black", "white", "red", "green", "blue", "yellow", "gray", "grey":
		return nil
	}
	if len(value) == 7 && value[0] == '#' {
		if _, err := hex.DecodeString(value[1:]); err == nil {
			return nil
		}
	}
	return fmt.Errorf("backgroundColor must be an opaque named color or #RRGGBB")
}

// MediaKind 是素材类别。
type MediaKind string

const (
	MediaVideo MediaKind = "video"
	MediaAudio MediaKind = "audio"
	MediaImage MediaKind = "image"
)

// Media 是已登记的素材引用。字节永远在 BlobStore，这里只有元数据。
type Media struct {
	MediaID string `json:"mediaId"`
	// Generation is assigned by the authoritative Timeline workflow. It
	// distinguishes delete/re-add cycles for the same mediaId. Zero is reserved
	// for legacy documents created before generations were introduced.
	Generation uint64    `json:"generation,omitempty"`
	URI        string    `json:"uri"`
	BlobKey    string    `json:"blobKey,omitempty"`
	Kind       MediaKind `json:"kind"`
	// DisplayName 是用户可编辑的展示名（updateMedia 写入）；缺省时前端按
	// BlobKey 的文件名兜底。
	DisplayName string `json:"displayName,omitempty"`
	// DurationInFrames 是素材总长换算到文档 fps 后的帧数。图片为 0（可无限拉伸）。
	DurationInFrames int64       `json:"durationInFrames"`
	Probe            *MediaProbe `json:"probe,omitempty"`
	// Transcript 是 ASR 转写结果，文本剪视频（删字=删片段）的数据基础。
	Transcript *Transcript `json:"transcript,omitempty"`
	// ThumbnailKey/ThumbnailURI：上传时由 cut-apiserver 本地 ffmpeg 生成的
	// 缩略图（视频为等间隔多帧横向 sprite，图片为单帧缩放），仅用于展示，
	// 不参与 Ops 坐标计算，缺失表示生成失败/不适用（best-effort，不阻断上传）。
	ThumbnailKey string `json:"thumbnailKey,omitempty"`
	ThumbnailURI string `json:"thumbnailUri,omitempty"`
	// WaveformPeaks 是按固定桶数（media.WaveformBucketCount）均分整段素材时长后，
	// 每桶归一化到 0~1 的振幅峰值，用于时间线上画波形；同样是展示层数据。
	WaveformPeaks []float32 `json:"waveformPeaks,omitempty"`
	// Processing 是异步预处理流水线的可观察状态。旧文档缺失该字段时仍然有效。
	Processing *MediaProcessing `json:"processing,omitempty"`
	// UnderstandingText 是 agentk.understand_media 对素材的异步理解结果。
	UnderstandingText string `json:"understandingText,omitempty"`
}

const MaxMediaIDLength = 128

// ValidateMediaID rejects IDs that could escape path-derived Blob keys.
func ValidateMediaID(id string) error {
	if len(id) < 1 || len(id) > MaxMediaIDLength {
		return fmt.Errorf("mediaId must be between 1 and %d characters", MaxMediaIDLength)
	}
	for _, c := range []byte(id) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("mediaId must match [A-Za-z0-9_-]{1,%d}", MaxMediaIDLength)
	}
	return nil
}

type ProcessingState string

const (
	ProcessingPending   ProcessingState = "pending"
	ProcessingRunning   ProcessingState = "running"
	ProcessingSucceeded ProcessingState = "succeeded"
	ProcessingSkipped   ProcessingState = "skipped"
	ProcessingFailed    ProcessingState = "failed"
)

type MediaProcessing struct {
	Status           string          `json:"status"` // processing | ready | partial | failed
	ReadyForTimeline bool            `json:"readyForTimeline"`
	Probe            ProcessingState `json:"probe"`
	Thumbnail        ProcessingState `json:"thumbnail"`
	Waveform         ProcessingState `json:"waveform"`
	Transcript       ProcessingState `json:"transcript"`
	Understanding    ProcessingState `json:"understanding"`
	Error            string          `json:"error,omitempty"`
}

// MediaProbe 是探测出的技术信息，仅用于诊断展示，不参与 Ops 坐标计算。
type MediaProbe struct {
	DurationMs int64   `json:"durationMs,omitempty"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	NativeFPS  float64 `json:"nativeFps,omitempty"`
	HasAudio   bool    `json:"hasAudio,omitempty"`
	Codec      string  `json:"codec,omitempty"`
}

// Transcript 是一份素材的带时间戳转写。时间用毫秒（源素材本地时间，
// 不是时间线帧），换算到帧空间由调用方按文档 fps 处理。
type Transcript struct {
	// Revision is assigned by the authoritative Timeline apply path and
	// increases whenever this source transcript is replaced. Zero denotes a
	// legacy transcript created before transcript revisions existed.
	Revision uint64              `json:"revision,omitempty"`
	Language string              `json:"language,omitempty"`
	Segments []TranscriptSegment `json:"segments"`
}

// TranscriptSegment 是一句转写。Words 可选（词级时间戳，删字=删片段的最小粒度）。
type TranscriptSegment struct {
	SegmentID string           `json:"segmentId,omitempty"`
	StartMs   int64            `json:"startMs"`
	EndMs     int64            `json:"endMs"`
	Text      string           `json:"text"`
	Words     []TranscriptWord `json:"words,omitempty"`
}

// TranscriptWord 是词级时间戳。
type TranscriptWord struct {
	WordID  string `json:"wordId,omitempty"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
}

// TrackKind 是轨道类别，决定轨道能容纳哪些 Item。
type TrackKind string

const (
	TrackVideo   TrackKind = "video"   // 容纳 video/image item
	TrackAudio   TrackKind = "audio"   // 容纳 audio item（配音、BGM）
	TrackCaption TrackKind = "caption" // 容纳 text item（字幕）
)

type TrackCreatedBy string

const (
	TrackCreatedAuto TrackCreatedBy = "auto"
	TrackCreatedUser TrackCreatedBy = "user"
)

// Track 是一条轨道。Tracks 切片的顺序即合成顺序（后者盖前者）。
type Track struct {
	TrackID   string         `json:"trackId"`
	Kind      TrackKind      `json:"kind"`
	Name      string         `json:"name,omitempty"`
	CreatedBy TrackCreatedBy `json:"createdBy,omitempty"`
	// Locked 为真时 user/agent 均不能改本轨内容。
	Locked bool `json:"locked,omitempty"`
	Muted  bool `json:"muted,omitempty"`
	// Hidden suppresses video-track pixels while preserving embedded audio.
	// The zero value keeps legacy documents visible.
	Hidden bool `json:"hidden,omitempty"`
	// Volume 是轨道级增益（0~2，0 表示未设置按 1 处理）。
	Volume float64 `json:"volume,omitempty"`
	// Ducking lowers this audio track while any configured sidechain track is
	// active. Nil preserves the legacy no-ducking behavior.
	Ducking *DuckingConfig `json:"ducking,omitempty"`
}

type DuckingConfig struct {
	SidechainTrackIDs []string `json:"sidechainTrackIds"`
	Gain              float64  `json:"gain"`
	AttackFrames      int64    `json:"attackFrames"`
	ReleaseFrames     int64    `json:"releaseFrames"`
}

// ItemKind 是片段类别。
type ItemKind string

const (
	ItemVideo ItemKind = "video"
	ItemAudio ItemKind = "audio"
	ItemImage ItemKind = "image"
	ItemText  ItemKind = "text" // 字幕/贴字，不引用 media
)

const (
	ItemFitContain = "contain"
	ItemFitCover   = "cover"
)

// ItemTransform describes a deterministic rectangular region on the output
// canvas. Coordinates are normalized to [0, 1]. Opacity zero is the legacy
// unset value and is rendered as fully opaque.
type ItemTransform struct {
	X               float64         `json:"x"`
	Y               float64         `json:"y"`
	Width           float64         `json:"width"`
	Height          float64         `json:"height"`
	Opacity         float64         `json:"opacity,omitempty"`
	Fit             string          `json:"fit,omitempty"`
	Crop            *NormalizedRect `json:"crop,omitempty"`
	RotationDegrees float64         `json:"rotationDegrees,omitempty"`
}

// NormalizedRect is a source-relative crop rectangle in [0, 1].
type NormalizedRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// CaptionStyle contains field-level overrides. Pointer-valued scalars preserve
// the distinction between an omitted override and an explicit false/zero.
type CaptionStyle struct {
	FontFamily      *string  `json:"fontFamily,omitempty"`
	FontSize        *float64 `json:"fontSize,omitempty"`
	TextColor       *string  `json:"textColor,omitempty"`
	BackgroundColor *string  `json:"backgroundColor,omitempty"`
	OutlineColor    *string  `json:"outlineColor,omitempty"`
	OutlineWidth    *float64 `json:"outlineWidth,omitempty"`
	Bold            *bool    `json:"bold,omitempty"`
	Italic          *bool    `json:"italic,omitempty"`
	Align           *string  `json:"align,omitempty"`
	X               *float64 `json:"x,omitempty"`
	Y               *float64 `json:"y,omitempty"`
	MaxWidth        *float64 `json:"maxWidth,omitempty"`
	LineHeight      *float64 `json:"lineHeight,omitempty"`
}

// Item 是时间线上的一个片段。
type Item struct {
	ItemID  string   `json:"itemId"`
	TrackID string   `json:"trackId"`
	Kind    ItemKind `json:"kind"`
	// MediaID 对 video/audio/image 必填，对 text 必须为空。
	MediaID string `json:"mediaId,omitempty"`
	// From 是片段在时间线上的起始帧（文档 fps 帧空间）。
	From int64 `json:"from"`
	// DurationInFrames 是片段在时间线上占据的帧数。
	DurationInFrames int64 `json:"durationInFrames"`
	// TrimStart 是源素材被裁掉的开头帧数（同样按文档 fps 表达）。
	TrimStart int64 `json:"trimStart,omitempty"`
	// Speed 是播放速率（0 表示未设置按 1 处理）。源素材消耗量 =
	// DurationInFrames * Speed。
	Speed float64 `json:"speed,omitempty"`
	// Volume 是片段级增益（0~2，0 表示未设置按 1 处理）。
	Volume float64 `json:"volume,omitempty"`
	// Muted is the only way to request silence. Volume zero remains the
	// backward-compatible unset value and therefore means gain 1.
	Muted         bool  `json:"muted,omitempty"`
	FadeInFrames  int64 `json:"fadeInFrames,omitempty"`
	FadeOutFrames int64 `json:"fadeOutFrames,omitempty"`
	// Transform is supported by video/image items. Nil means a full-canvas
	// contain transform.
	Transform *ItemTransform `json:"transform,omitempty"`
	// Text 仅 text item 使用。
	Text string `json:"text,omitempty"`
	// StylePresetID 引用前后端共享的字幕/贴字样式预设。
	StylePresetID string `json:"stylePresetId,omitempty"`
	// CaptionStyle overrides individual preset fields for text items.
	CaptionStyle *CaptionStyle `json:"captionStyle,omitempty"`
}

const (
	TransitionCrossfade  = "crossfade"
	TransitionDipToBlack = "dipToBlack"
)

// Transition describes a visual transition at an exact item boundary.
type Transition struct {
	TransitionID string `json:"transitionId"`
	FromItemID   string `json:"fromItemId"`
	ToItemID     string `json:"toItemId"`
	Kind         string `json:"kind"`
	Frames       int64  `json:"frames"`
}

// NewDoc 返回一份空文档。
func NewDoc(name string, width, height, fps int) Doc {
	return Doc{
		SchemaVersion: SchemaVersion,
		Metadata:      Metadata{Name: name, Width: width, Height: height, FPS: fps},
		Media:         []Media{},
		Tracks:        []Track{},
		Items:         []Item{},
	}
}

// EffectiveSpeed 返回归一化后的速率。
func (it Item) EffectiveSpeed() float64 {
	if it.Speed <= 0 {
		return 1
	}
	return it.Speed
}

func (it Item) EffectiveOpacity() float64 {
	if it.Transform == nil || it.Transform.Opacity == 0 {
		return 1
	}
	return it.Transform.Opacity
}

func (it Item) EffectiveFit() string {
	if it.Transform == nil || it.Transform.Fit == "" {
		return ItemFitContain
	}
	return it.Transform.Fit
}

// End 返回片段在时间线上的结束帧（开区间）。
func (it Item) End() int64 { return it.From + it.DurationInFrames }

// SourceFramesConsumed 返回片段消耗的源素材帧数（按文档 fps）。
func (it Item) SourceFramesConsumed() int64 {
	return roundFrames(float64(it.DurationInFrames) * it.EffectiveSpeed())
}

func exactSourceFrameDelta(timelineFrames int64, speed float64) (int64, bool) {
	value := float64(timelineFrames) * speed
	rounded := math.Round(value)
	return int64(rounded), math.Abs(value-rounded) < 1e-9
}

// roundFrames 是全库统一的帧数四舍五入（对负数也正确），保证确定性。
func roundFrames(f float64) int64 { return int64(math.Round(f)) }

func (d Doc) mediaByID(id string) (int, bool) {
	for i := range d.Media {
		if d.Media[i].MediaID == id {
			return i, true
		}
	}
	return -1, false
}

func (d Doc) trackByID(id string) (int, bool) {
	for i := range d.Tracks {
		if d.Tracks[i].TrackID == id {
			return i, true
		}
	}
	return -1, false
}

func (d Doc) itemByID(id string) (int, bool) {
	for i := range d.Items {
		if d.Items[i].ItemID == id {
			return i, true
		}
	}
	return -1, false
}

func (d Doc) transitionByID(id string) (int, bool) {
	for i := range d.Transitions {
		if d.Transitions[i].TransitionID == id {
			return i, true
		}
	}
	return -1, false
}

// itemKindAllowedOn 判断 item 类别能否放进 track 类别。
func itemKindAllowedOn(item ItemKind, track TrackKind) bool {
	switch track {
	case TrackVideo:
		return item == ItemVideo || item == ItemImage
	case TrackAudio:
		return item == ItemAudio
	case TrackCaption:
		return item == ItemText
	}
	return false
}

// mediaKindForItem 返回 item 类别要求的 media 类别。
func mediaKindForItem(kind ItemKind) (MediaKind, bool) {
	switch kind {
	case ItemVideo:
		return MediaVideo, true
	case ItemAudio:
		return MediaAudio, true
	case ItemImage:
		return MediaImage, true
	}
	return "", false
}

// Clone 深拷贝文档（ApplyOps 的输入保护）。
func (d Doc) Clone() Doc {
	out := d
	out.Media = make([]Media, len(d.Media))
	for i, m := range d.Media {
		out.Media[i] = m
		if m.WaveformPeaks != nil {
			out.Media[i].WaveformPeaks = append([]float32(nil), m.WaveformPeaks...)
		}
		if m.Probe != nil {
			p := *m.Probe
			out.Media[i].Probe = &p
		}
		if m.Transcript != nil {
			t := Transcript{Revision: m.Transcript.Revision, Language: m.Transcript.Language}
			t.Segments = make([]TranscriptSegment, len(m.Transcript.Segments))
			for j, s := range m.Transcript.Segments {
				t.Segments[j] = s
				if s.Words != nil {
					t.Segments[j].Words = append([]TranscriptWord(nil), s.Words...)
				}
			}
			out.Media[i].Transcript = &t
		}
		if m.Processing != nil {
			p := *m.Processing
			out.Media[i].Processing = &p
		}
	}
	out.Tracks = make([]Track, len(d.Tracks))
	for i, track := range d.Tracks {
		out.Tracks[i] = track
		if track.Ducking != nil {
			ducking := *track.Ducking
			ducking.SidechainTrackIDs = append([]string(nil), track.Ducking.SidechainTrackIDs...)
			out.Tracks[i].Ducking = &ducking
		}
	}
	out.Items = make([]Item, len(d.Items))
	for i, item := range d.Items {
		out.Items[i] = cloneItem(item)
	}
	out.Transitions = append([]Transition(nil), d.Transitions...)
	return out
}

func cloneItem(item Item) Item {
	out := item
	if item.Transform != nil {
		transform := *item.Transform
		if item.Transform.Crop != nil {
			crop := *item.Transform.Crop
			transform.Crop = &crop
		}
		out.Transform = &transform
	}
	out.CaptionStyle = cloneCaptionStyle(item.CaptionStyle)
	return out
}

func cloneCaptionStyle(style *CaptionStyle) *CaptionStyle {
	if style == nil {
		return nil
	}
	out := *style
	if style.FontFamily != nil {
		value := *style.FontFamily
		out.FontFamily = &value
	}
	if style.FontSize != nil {
		value := *style.FontSize
		out.FontSize = &value
	}
	if style.TextColor != nil {
		value := *style.TextColor
		out.TextColor = &value
	}
	if style.BackgroundColor != nil {
		value := *style.BackgroundColor
		out.BackgroundColor = &value
	}
	if style.OutlineColor != nil {
		value := *style.OutlineColor
		out.OutlineColor = &value
	}
	if style.OutlineWidth != nil {
		value := *style.OutlineWidth
		out.OutlineWidth = &value
	}
	if style.Bold != nil {
		value := *style.Bold
		out.Bold = &value
	}
	if style.Italic != nil {
		value := *style.Italic
		out.Italic = &value
	}
	if style.Align != nil {
		value := *style.Align
		out.Align = &value
	}
	if style.X != nil {
		value := *style.X
		out.X = &value
	}
	if style.Y != nil {
		value := *style.Y
		out.Y = &value
	}
	if style.MaxWidth != nil {
		value := *style.MaxWidth
		out.MaxWidth = &value
	}
	if style.LineHeight != nil {
		value := *style.LineHeight
		out.LineHeight = &value
	}
	return &out
}
