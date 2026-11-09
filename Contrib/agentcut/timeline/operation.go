package timeline

// Op 名称常量。三类来源（前端 user / Agent tool / 系统上传链路）共用同一协议。
const (
	OpAddMedia         = "addMedia"
	OpRemoveMedia      = "removeMedia"
	OpUpdateMedia      = "updateMedia"
	OpSetTranscript    = "setTranscript"
	OpAddItem          = "addItem"
	OpRemoveItems      = "removeItems"
	OpSplit            = "split"
	OpTrimStart        = "trimStart"
	OpTrimEnd          = "trimEnd"
	OpMoveItem         = "moveItem"
	OpUpdateItem       = "updateItem"
	OpRippleDelete     = "rippleDelete"
	OpAddTrack         = "addTrack"
	OpRemoveTrack      = "removeTrack"
	OpUpdateTrack      = "updateTrack"
	OpSetCaptions      = "setCaptions"
	OpSetMetadata      = "setMetadata"
	OpAddTransition    = "addTransition"
	OpUpdateTransition = "updateTransition"
	OpRemoveTransition = "removeTransition"
)

// Operation 是所有 op 的扁平判别联合：`op` 字段决定使用哪些字段。
// 用扁平结构而不是接口，为的是 JSON 形状简单稳定，前端 TypeScript
// 可以从同一份 Schema 生成完全一致的类型。
type Operation struct {
	Op string `json:"op"`

	// addMedia
	Media *Media `json:"media,omitempty"`
	// removeMedia / setTranscript / updateMedia
	MediaID string `json:"mediaId,omitempty"`
	// MediaGeneration makes system preprocessing updates conditional on the
	// exact media incarnation. A non-nil zero targets legacy generation 0.
	MediaGeneration *uint64 `json:"mediaGeneration,omitempty"`
	// setTranscript
	Transcript *Transcript `json:"transcript,omitempty"`
	// updateMedia 白名单 patch
	MediaPatch *MediaPatch `json:"mediaPatch,omitempty"`

	// addItem
	Item *Item `json:"item,omitempty"`
	// removeItems
	ItemIDs []string `json:"itemIds,omitempty"`

	// split / trimStart / trimEnd / moveItem / updateItem
	ItemID string `json:"itemId,omitempty"`
	// split：分割点；trimStart：新左沿；trimEnd：新右沿（开区间）
	Frame *int64 `json:"frame,omitempty"`
	// split 产生的右侧新 item ID；缺省按 "{itemId}~{frame}" 确定性派生
	NewItemID string `json:"newItemId,omitempty"`

	// moveItem（TrackID 可选：跨轨移动）；addTrack/removeTrack/updateTrack/setCaptions 也用 TrackID
	TrackID string `json:"trackId,omitempty"`
	From    *int64 `json:"from,omitempty"`

	// updateItem 白名单 patch
	Patch *ItemPatch `json:"patch,omitempty"`

	// rippleDelete：[fromFrame, toFrame) 区间删除并把后续内容左移
	FromFrame *int64 `json:"fromFrame,omitempty"`
	ToFrame   *int64 `json:"toFrame,omitempty"`
	// rippleDelete 作用的轨道；空 = 所有未锁定轨道
	TrackIDs []string `json:"trackIds,omitempty"`

	// addTrack
	Track *Track `json:"track,omitempty"`
	Index *int   `json:"index,omitempty"`

	// updateTrack
	TrackPatch *TrackPatch `json:"trackPatch,omitempty"`

	// setCaptions：整轨替换
	Captions []Caption `json:"captions,omitempty"`

	// setMetadata
	Metadata *MetadataPatch `json:"metadata,omitempty"`

	// addTransition
	Transition *Transition `json:"transition,omitempty"`
	// updateTransition / removeTransition
	TransitionID string `json:"transitionId,omitempty"`
	// updateTransition
	TransitionPatch *TransitionPatch `json:"transitionPatch,omitempty"`
}

// MediaPatch 是 updateMedia 的白名单字段。nil 表示不修改。
type MediaPatch struct {
	DisplayName       *string          `json:"displayName,omitempty"`
	URI               *string          `json:"uri,omitempty"`
	BlobKey           *string          `json:"blobKey,omitempty"`
	DurationInFrames  *int64           `json:"durationInFrames,omitempty"`
	Probe             *MediaProbe      `json:"probe,omitempty"`
	ThumbnailKey      *string          `json:"thumbnailKey,omitempty"`
	ThumbnailURI      *string          `json:"thumbnailUri,omitempty"`
	WaveformPeaks     *[]float32       `json:"waveformPeaks,omitempty"`
	Transcript        *Transcript      `json:"transcript,omitempty"`
	Processing        *MediaProcessing `json:"processing,omitempty"`
	UnderstandingText *string          `json:"understandingText,omitempty"`
}

// ItemPatch 是 updateItem 的白名单字段。nil 表示不修改。
type ItemPatch struct {
	Speed         *float64       `json:"speed,omitempty"`
	Volume        *float64       `json:"volume,omitempty"`
	Muted         *bool          `json:"muted,omitempty"`
	FadeInFrames  *int64         `json:"fadeInFrames,omitempty"`
	FadeOutFrames *int64         `json:"fadeOutFrames,omitempty"`
	Transform     *ItemTransform `json:"transform,omitempty"`
	Text          *string        `json:"text,omitempty"`
	StylePresetID *string        `json:"stylePresetId,omitempty"`
	CaptionStyle  *CaptionStyle  `json:"captionStyle,omitempty"`
}

// TrackPatch 是 updateTrack 的白名单字段。
type TrackPatch struct {
	Name    *string        `json:"name,omitempty"`
	Locked  *bool          `json:"locked,omitempty"`
	Muted   *bool          `json:"muted,omitempty"`
	Hidden  *bool          `json:"hidden,omitempty"`
	Volume  *float64       `json:"volume,omitempty"`
	Ducking *DuckingConfig `json:"ducking,omitempty"`
}

// TransitionPatch is the updateTransition whitelist. Boundary item IDs and the
// transition ID are immutable; remove and re-add to target another boundary.
type TransitionPatch struct {
	Kind   *string `json:"kind,omitempty"`
	Frames *int64  `json:"frames,omitempty"`
}

// Caption 是 setCaptions 的一条字幕；item ID 由服务端确定性生成。
type Caption struct {
	From             int64         `json:"from"`
	DurationInFrames int64         `json:"durationInFrames"`
	Text             string        `json:"text"`
	StylePresetID    string        `json:"stylePresetId,omitempty"`
	CaptionStyle     *CaptionStyle `json:"captionStyle,omitempty"`
}

// MetadataPatch 是 setMetadata 的白名单字段。
type MetadataPatch struct {
	Name            *string `json:"name,omitempty"`
	Width           *int    `json:"width,omitempty"`
	Height          *int    `json:"height,omitempty"`
	FPS             *int    `json:"fps,omitempty"`
	BackgroundColor *string `json:"backgroundColor,omitempty"`
}

// ChangeSummary 汇报一批 Ops 实际改动了什么，用于前端高亮与 Agent 汇报。
type ChangeSummary struct {
	AddedItemIDs         []string `json:"addedItemIds,omitempty"`
	RemovedItemIDs       []string `json:"removedItemIds,omitempty"`
	UpdatedItemIDs       []string `json:"updatedItemIds,omitempty"`
	AddedTrackIDs        []string `json:"addedTrackIds,omitempty"`
	RemovedTrackIDs      []string `json:"removedTrackIds,omitempty"`
	UpdatedTrackIDs      []string `json:"updatedTrackIds,omitempty"`
	AddedMediaIDs        []string `json:"addedMediaIds,omitempty"`
	RemovedMediaIDs      []string `json:"removedMediaIds,omitempty"`
	UpdatedMediaIDs      []string `json:"updatedMediaIds,omitempty"`
	AddedTransitionIDs   []string `json:"addedTransitionIds,omitempty"`
	RemovedTransitionIDs []string `json:"removedTransitionIds,omitempty"`
	UpdatedTransitionIDs []string `json:"updatedTransitionIds,omitempty"`
	MetadataChanged      bool     `json:"metadataChanged,omitempty"`
}
