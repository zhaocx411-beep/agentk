package timeline

import (
	"encoding/json"
	"math"
	"sort"
)

// ValidateDoc 对整份文档做不变量校验。ApplyOps 在整批 op 应用完后调用；
// 从快照恢复、外部导入等场景也应调用。返回 *OpError（Index=-1 表示批级）。
func ValidateDoc(d Doc) *OpError {
	if d.SchemaVersion != SchemaVersion {
		return batchErrf(ErrUnsupported, "schemaVersion %d unsupported", d.SchemaVersion)
	}
	if !AllowedFPS[d.Metadata.FPS] {
		return batchErrf(ErrBadRequest, "metadata.fps %d not allowed", d.Metadata.FPS)
	}
	if d.Metadata.Width < 16 || d.Metadata.Width > 8192 || d.Metadata.Height < 16 || d.Metadata.Height > 8192 {
		return batchErrf(ErrBadRequest, "metadata canvas %dx%d out of range", d.Metadata.Width, d.Metadata.Height)
	}
	if err := ValidateBackgroundColor(d.Metadata.BackgroundColor); err != nil {
		return batchErrf(ErrBadRequest, "%v", err)
	}
	if len(d.Media) > MaxMedia || len(d.Tracks) > MaxTracks || len(d.Items) > MaxItems ||
		len(d.Transitions) > MaxTransitions {
		return batchErrf(ErrLimitExceeded,
			"document exceeds size limits (media=%d tracks=%d items=%d transitions=%d)",
			len(d.Media), len(d.Tracks), len(d.Items), len(d.Transitions))
	}

	// 唯一性
	mediaIDs := make(map[string]bool, len(d.Media))
	for _, m := range d.Media {
		if err := ValidateMediaID(m.MediaID); err != nil {
			return batchErrf(ErrInvariant, "invalid mediaId %q: %v", m.MediaID, err)
		}
		if mediaIDs[m.MediaID] {
			return batchErrf(ErrInvariant, "duplicate or empty mediaId %q", m.MediaID)
		}
		mediaIDs[m.MediaID] = true
	}
	trackIDs := make(map[string]bool, len(d.Tracks))
	for _, t := range d.Tracks {
		if t.TrackID == "" || trackIDs[t.TrackID] {
			return batchErrf(ErrInvariant, "duplicate or empty trackId %q", t.TrackID)
		}
		trackIDs[t.TrackID] = true
		switch t.Kind {
		case TrackVideo, TrackAudio, TrackCaption:
		default:
			return batchErrf(ErrInvariant, "track %q has invalid kind %q", t.TrackID, t.Kind)
		}
		switch t.CreatedBy {
		case "", TrackCreatedAuto, TrackCreatedUser:
		default:
			return batchErrf(
				ErrInvariant, "track %q has invalid createdBy %q", t.TrackID, t.CreatedBy,
			)
		}
		if t.Hidden && t.Kind != TrackVideo {
			return batchErrf(ErrInvariant, "track %q hidden only applies to video tracks", t.TrackID)
		}
		if math.IsNaN(t.Volume) || math.IsInf(t.Volume, 0) || t.Volume < 0 || t.Volume > 2 {
			return batchErrf(ErrInvariant, "track %q volume must be finite and within [0, 2]", t.TrackID)
		}
	}
	for _, t := range d.Tracks {
		if err := validateDuckingConfig(&d, -1, "", t); err != nil {
			return batchErrf(ErrInvariant, "track %q ducking invalid: %s", t.TrackID, err.Message)
		}
	}
	itemIDs := make(map[string]bool, len(d.Items))
	for _, it := range d.Items {
		if it.ItemID == "" || itemIDs[it.ItemID] {
			return batchErrf(ErrInvariant, "duplicate or empty itemId %q", it.ItemID)
		}
		itemIDs[it.ItemID] = true
	}

	transitionIDs := make(map[string]bool, len(d.Transitions))
	for index, transition := range d.Transitions {
		if transition.TransitionID == "" || transitionIDs[transition.TransitionID] {
			return batchErrf(ErrInvariant, "duplicate or empty transitionId %q", transition.TransitionID)
		}
		transitionIDs[transition.TransitionID] = true
		if err := validateTransitionShape(&d, -1, "", transition, index); err != nil {
			return batchErrf(ErrInvariant, "transition %q invalid: %s", transition.TransitionID, err.Message)
		}
	}

	// 引用、类别、几何（复用单 op 的 shape 校验）
	for _, it := range d.Items {
		if err := validateItemShape(&d, -1, "", it); err != nil {
			return batchErrf(ErrInvariant, "item %q invalid: %s", it.ItemID, err.Message)
		}
	}

	// 同轨禁止重叠。按 Tracks 声明顺序遍历（不能用 map 迭代：顺序随机，
	// 会破坏 Workflow replay 确定性）。
	for _, tr := range d.Tracks {
		trackID := tr.TrackID
		var items []Item
		for _, it := range d.Items {
			if it.TrackID == trackID {
				items = append(items, it)
			}
		}
		sort.SliceStable(items, func(a, b int) bool {
			if items[a].From != items[b].From {
				return items[a].From < items[b].From
			}
			return items[a].ItemID < items[b].ItemID
		})
		for k := 1; k < len(items); k++ {
			if items[k].From < items[k-1].End() {
				return batchErrf(ErrOverlap, "items %q and %q overlap on track %q",
					items[k-1].ItemID, items[k].ItemID, trackID)
			}
		}
	}

	// 文档体积（早于 Temporal payload 上限在应用层拦截）
	raw, err := json.Marshal(d)
	if err != nil {
		return batchErrf(ErrInvariant, "document not serializable: %v", err)
	}
	if len(raw) > MaxDocBytes {
		return batchErrf(ErrLimitExceeded, "document size %d exceeds %d bytes", len(raw), MaxDocBytes)
	}
	return nil
}

// DocDurationInFrames 返回时间线总长（最右 item 的结束帧）。
func DocDurationInFrames(d Doc) int64 {
	var max int64
	for _, it := range d.Items {
		if end := it.End(); end > max {
			max = end
		}
	}
	return max
}
