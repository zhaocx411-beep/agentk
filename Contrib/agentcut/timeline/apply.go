package timeline

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// ApplyOps 把一批操作应用到文档上，返回新文档与改动摘要。
//
// 约束（必须始终成立）：
//   - 纯函数：不读存储、不发事件、无时间/随机数/网络（在 Temporal Workflow 内执行）；
//   - 输入 doc 不被修改；
//   - 任一 op 失败则整批不生效，返回原因（*OpError）；
//   - 批内允许临时不一致（如先重叠后移开），批后整体校验必须通过。
func ApplyOps(doc Doc, ops []Operation) (Doc, ChangeSummary, error) {
	var sum ChangeSummary
	if len(ops) == 0 {
		return doc, sum, batchErrf(ErrBadRequest, "empty operations")
	}
	if len(ops) > MaxOpsPerBatch {
		return doc, sum, batchErrf(ErrLimitExceeded, "operations %d exceeds max %d", len(ops), MaxOpsPerBatch)
	}
	if doc.SchemaVersion != SchemaVersion {
		return doc, sum, batchErrf(ErrUnsupported, "schemaVersion %d unsupported", doc.SchemaVersion)
	}

	next := doc.Clone()
	rec := &recorder{}
	for i, op := range ops {
		var err *OpError
		switch op.Op {
		case OpAddMedia:
			err = applyAddMedia(&next, i, op, rec)
		case OpRemoveMedia:
			err = applyRemoveMedia(&next, i, op, rec)
		case OpUpdateMedia:
			err = applyUpdateMedia(&next, i, op, rec)
		case OpSetTranscript:
			err = applySetTranscript(&next, i, op, rec)
		case OpAddItem:
			err = applyAddItem(&next, i, op, rec)
		case OpRemoveItems:
			err = applyRemoveItems(&next, i, op, rec)
		case OpSplit:
			err = applySplit(&next, i, op, rec)
		case OpTrimStart:
			err = applyTrim(&next, i, op, rec, true)
		case OpTrimEnd:
			err = applyTrim(&next, i, op, rec, false)
		case OpMoveItem:
			err = applyMoveItem(&next, i, op, rec)
		case OpUpdateItem:
			err = applyUpdateItem(&next, i, op, rec)
		case OpRippleDelete:
			err = applyRippleDelete(&next, i, op, rec)
		case OpAddTrack:
			err = applyAddTrack(&next, i, op, rec)
		case OpRemoveTrack:
			err = applyRemoveTrack(&next, i, op, rec)
		case OpUpdateTrack:
			err = applyUpdateTrack(&next, i, op, rec)
		case OpSetCaptions:
			err = applySetCaptions(&next, i, op, rec)
		case OpSetMetadata:
			err = applySetMetadata(&next, i, op, rec)
		case OpAddTransition:
			err = applyAddTransition(&next, i, op, rec)
		case OpUpdateTransition:
			err = applyUpdateTransition(&next, i, op, rec)
		case OpRemoveTransition:
			err = applyRemoveTransition(&next, i, op, rec)
		default:
			err = opErrf(i, op.Op, ErrUnsupported, "unknown op %q", op.Op)
		}
		if err != nil {
			return doc, ChangeSummary{}, err
		}
	}
	// Geometry-changing operations may temporarily invalidate adjacency inside a
	// batch (for example, moving both sides of a boundary). Cascade only after
	// all operations have established the authoritative final geometry.
	removeInvalidTransitions(&next, rec)

	if verr := ValidateDoc(next); verr != nil {
		return doc, ChangeSummary{}, verr
	}
	return next, rec.summary(), nil
}

// recorder 收集改动，最后去重汇总成 ChangeSummary。
type recorder struct {
	addedItems, removedItems, updatedItems                   []string
	addedTracks, removedTracks, updatedTracks                []string
	addedMedia, removedMedia, updatedMedia                   []string
	addedTransitions, removedTransitions, updatedTransitions []string
	metadataChanged                                          bool
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (r *recorder) summary() ChangeSummary {
	// 同批内先加后删的 ID 两边都会出现，保持真实轨迹，不做互相抵消。
	return ChangeSummary{
		AddedItemIDs:         dedupe(r.addedItems),
		RemovedItemIDs:       dedupe(r.removedItems),
		UpdatedItemIDs:       dedupe(r.updatedItems),
		AddedTrackIDs:        dedupe(r.addedTracks),
		RemovedTrackIDs:      dedupe(r.removedTracks),
		UpdatedTrackIDs:      dedupe(r.updatedTracks),
		AddedMediaIDs:        dedupe(r.addedMedia),
		RemovedMediaIDs:      dedupe(r.removedMedia),
		UpdatedMediaIDs:      dedupe(r.updatedMedia),
		AddedTransitionIDs:   dedupe(r.addedTransitions),
		RemovedTransitionIDs: dedupe(r.removedTransitions),
		UpdatedTransitionIDs: dedupe(r.updatedTransitions),
		MetadataChanged:      r.metadataChanged,
	}
}

// requireUnlockedTrack 校验轨道存在且未锁定，返回轨道下标。
func requireUnlockedTrack(d *Doc, i int, opName, trackID string) (int, *OpError) {
	ti, ok := d.trackByID(trackID)
	if !ok {
		return -1, opErrf(i, opName, ErrNotFound, "track %q not found", trackID)
	}
	if d.Tracks[ti].Locked {
		return -1, opErrf(i, opName, ErrLockedTrack, "track %q is locked", trackID)
	}
	return ti, nil
}

// requireUnlockedItem 校验 item 存在且其所在轨道未锁定，返回 item 下标。
func requireUnlockedItem(d *Doc, i int, opName, itemID string) (int, *OpError) {
	ii, ok := d.itemByID(itemID)
	if !ok {
		return -1, opErrf(i, opName, ErrNotFound, "item %q not found", itemID)
	}
	if ti, ok := d.trackByID(d.Items[ii].TrackID); ok && d.Tracks[ti].Locked {
		return -1, opErrf(i, opName, ErrLockedTrack, "track %q is locked", d.Items[ii].TrackID)
	}
	return ii, nil
}

// ---- media ----

func applyAddMedia(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Media == nil {
		return opErrf(i, op.Op, ErrBadRequest, "media is required")
	}
	m := *op.Media
	if err := ValidateMediaID(m.MediaID); err != nil {
		return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
	}
	if m.URI == "" {
		return opErrf(i, op.Op, ErrBadRequest, "uri is required")
	}
	if _, exists := d.mediaByID(m.MediaID); exists {
		return opErrf(i, op.Op, ErrConflictingID, "media %q already exists", m.MediaID)
	}
	switch m.Kind {
	case MediaVideo, MediaAudio:
		if m.DurationInFrames <= 0 {
			return opErrf(i, op.Op, ErrBadRequest, "%s media requires durationInFrames > 0", m.Kind)
		}
	case MediaImage:
		if m.DurationInFrames != 0 {
			return opErrf(i, op.Op, ErrBadRequest, "image media must have durationInFrames = 0")
		}
	default:
		return opErrf(i, op.Op, ErrBadRequest, "unknown media kind %q", m.Kind)
	}
	if len(d.Media) >= MaxMedia {
		return opErrf(i, op.Op, ErrLimitExceeded, "media count would exceed %d", MaxMedia)
	}
	if m.Transcript != nil {
		normalized, err := normalizeTranscriptReplacement(
			m.MediaID, m.Generation, nil, m.Transcript,
		)
		if err != nil {
			return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
		}
		m.Transcript = normalized
	}
	d.Media = append(d.Media, m)
	rec.addedMedia = append(rec.addedMedia, m.MediaID)
	return nil
}

func applyRemoveMedia(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if err := ValidateMediaID(op.MediaID); err != nil {
		return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
	}
	mi, ok := d.mediaByID(op.MediaID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "media %q not found", op.MediaID)
	}
	for _, it := range d.Items {
		if it.MediaID == op.MediaID {
			return opErrf(i, op.Op, ErrStillReferenced, "media %q is referenced by item %q", op.MediaID, it.ItemID)
		}
	}
	d.Media = append(d.Media[:mi], d.Media[mi+1:]...)
	rec.removedMedia = append(rec.removedMedia, op.MediaID)
	return nil
}

func applyUpdateMedia(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if err := ValidateMediaID(op.MediaID); err != nil {
		return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
	}
	if op.MediaPatch == nil {
		return opErrf(i, op.Op, ErrBadRequest, "mediaPatch is required")
	}
	mi, ok := d.mediaByID(op.MediaID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "media %q not found", op.MediaID)
	}
	if op.MediaGeneration != nil && d.Media[mi].Generation != *op.MediaGeneration {
		return opErrf(i, op.Op, ErrConflictingID,
			"media %q generation %d no longer matches expected %d",
			op.MediaID, d.Media[mi].Generation, *op.MediaGeneration)
	}
	p := op.MediaPatch
	if p.DisplayName == nil && p.URI == nil && p.BlobKey == nil &&
		p.DurationInFrames == nil && p.Probe == nil &&
		p.ThumbnailKey == nil && p.ThumbnailURI == nil && p.WaveformPeaks == nil &&
		p.Transcript == nil && p.Processing == nil && p.UnderstandingText == nil {
		return opErrf(i, op.Op, ErrBadRequest, "mediaPatch must update at least one field")
	}
	if p.DisplayName != nil {
		// 空白字符串是显式"清空覆盖名，回退到默认命名"，不是错误——前端撤销
		// 首次重命名时需要能精确恢复到"未设置"状态。
		name := strings.TrimSpace(*p.DisplayName)
		if len(name) > 256 {
			return opErrf(i, op.Op, ErrBadRequest, "displayName must be at most 256 characters")
		}
		d.Media[mi].DisplayName = name
	}
	if p.URI != nil {
		uri := strings.TrimSpace(*p.URI)
		if uri == "" || len(uri) > 4096 {
			return opErrf(i, op.Op, ErrBadRequest, "uri must be between 1 and 4096 characters")
		}
		d.Media[mi].URI = uri
	}
	if p.BlobKey != nil {
		key := strings.TrimSpace(strings.ReplaceAll(*p.BlobKey, "\\", "/"))
		if key == "" || len(key) > 2048 || strings.HasPrefix(key, "/") ||
			strings.Contains(key, "://") || strings.Contains("/"+key+"/", "/../") {
			return opErrf(i, op.Op, ErrBadRequest, "blobKey is not a valid managed object key")
		}
		d.Media[mi].BlobKey = key
	}
	if p.DurationInFrames != nil {
		switch d.Media[mi].Kind {
		case MediaVideo, MediaAudio:
			if *p.DurationInFrames <= 0 {
				return opErrf(i, op.Op, ErrBadRequest, "durationInFrames must be positive")
			}
		case MediaImage:
			if *p.DurationInFrames != 0 {
				return opErrf(i, op.Op, ErrBadRequest, "image durationInFrames must be zero")
			}
		}
		d.Media[mi].DurationInFrames = *p.DurationInFrames
	}
	if p.Probe != nil {
		if p.Probe.DurationMs <= 0 {
			return opErrf(i, op.Op, ErrBadRequest, "probe durationMs must be positive")
		}
		probe := *p.Probe
		d.Media[mi].Probe = &probe
	}
	if p.ThumbnailKey != nil {
		d.Media[mi].ThumbnailKey = strings.TrimSpace(*p.ThumbnailKey)
	}
	if p.ThumbnailURI != nil {
		d.Media[mi].ThumbnailURI = strings.TrimSpace(*p.ThumbnailURI)
	}
	if p.WaveformPeaks != nil {
		if len(*p.WaveformPeaks) > 10_000 {
			return opErrf(i, op.Op, ErrLimitExceeded, "waveformPeaks exceeds 10000 entries")
		}
		peaks := make([]float32, len(*p.WaveformPeaks))
		for index, peak := range *p.WaveformPeaks {
			if math.IsNaN(float64(peak)) || math.IsInf(float64(peak), 0) || peak < 0 || peak > 1 {
				return opErrf(i, op.Op, ErrBadRequest, "waveformPeaks[%d] must be between 0 and 1", index)
			}
			peaks[index] = peak
		}
		d.Media[mi].WaveformPeaks = peaks
	}
	if p.Transcript != nil {
		normalized, err := normalizeTranscriptReplacement(
			d.Media[mi].MediaID,
			d.Media[mi].Generation,
			d.Media[mi].Transcript,
			p.Transcript,
		)
		if err != nil {
			return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
		}
		d.Media[mi].Transcript = normalized
	}
	if p.Processing != nil {
		if err := validateMediaProcessing(i, op.Op, p.Processing); err != nil {
			return err
		}
		processing := *p.Processing
		processing.Error = strings.TrimSpace(processing.Error)
		d.Media[mi].Processing = &processing
	}
	if p.UnderstandingText != nil {
		if len(*p.UnderstandingText) > 1<<20 {
			return opErrf(i, op.Op, ErrLimitExceeded, "understandingText exceeds 1 MiB")
		}
		d.Media[mi].UnderstandingText = strings.TrimSpace(*p.UnderstandingText)
	}
	rec.updatedMedia = append(rec.updatedMedia, op.MediaID)
	return nil
}

func applySetTranscript(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if err := ValidateMediaID(op.MediaID); err != nil {
		return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
	}
	mi, ok := d.mediaByID(op.MediaID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "media %q not found", op.MediaID)
	}
	if op.Transcript == nil {
		return opErrf(i, op.Op, ErrBadRequest, "transcript is required")
	}
	normalized, err := normalizeTranscriptReplacement(
		d.Media[mi].MediaID,
		d.Media[mi].Generation,
		d.Media[mi].Transcript,
		op.Transcript,
	)
	if err != nil {
		return opErrf(i, op.Op, ErrBadRequest, "%s", err.Error())
	}
	d.Media[mi].Transcript = normalized
	rec.updatedMedia = append(rec.updatedMedia, op.MediaID)
	return nil
}

func validateTranscript(i int, opName string, transcript *Transcript) *OpError {
	if err := validateTranscriptContents(transcript); err != nil {
		return opErrf(i, opName, ErrBadRequest, "%s", err.Error())
	}
	return nil
}

func cloneTranscript(transcript *Transcript) *Transcript {
	out := &Transcript{
		Revision: transcript.Revision,
		Language: transcript.Language,
		Segments: make([]TranscriptSegment, len(transcript.Segments)),
	}
	for i, segment := range transcript.Segments {
		out.Segments[i] = segment
		out.Segments[i].Words = append([]TranscriptWord(nil), segment.Words...)
	}
	return out
}

func validateMediaProcessing(i int, opName string, processing *MediaProcessing) *OpError {
	switch processing.Status {
	case "processing", "ready", "partial", "failed":
	default:
		return opErrf(i, opName, ErrBadRequest, "invalid processing status %q", processing.Status)
	}
	states := []struct {
		name  string
		state ProcessingState
	}{
		{"probe", processing.Probe},
		{"thumbnail", processing.Thumbnail},
		{"waveform", processing.Waveform},
		{"transcript", processing.Transcript},
		{"understanding", processing.Understanding},
	}
	for _, entry := range states {
		switch entry.state {
		case ProcessingPending, ProcessingRunning, ProcessingSucceeded, ProcessingSkipped, ProcessingFailed:
		default:
			return opErrf(i, opName, ErrBadRequest, "invalid %s processing state %q", entry.name, entry.state)
		}
	}
	if processing.Status == "failed" && processing.ReadyForTimeline {
		return opErrf(i, opName, ErrBadRequest, "failed processing cannot be readyForTimeline")
	}
	return nil
}

// ---- items ----

// validateItemShape 校验 item 自身字段（引用与几何在这里，重叠在批后整体校验）。
func validateItemShape(d *Doc, i int, opName string, it Item) *OpError {
	if it.ItemID == "" {
		return opErrf(i, opName, ErrBadRequest, "itemId is required")
	}
	ti, ok := d.trackByID(it.TrackID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "track %q not found", it.TrackID)
	}
	track := d.Tracks[ti]
	if !itemKindAllowedOn(it.Kind, track.Kind) {
		return opErrf(i, opName, ErrKindMismatch, "%s item not allowed on %s track", it.Kind, track.Kind)
	}
	if it.From < 0 || it.DurationInFrames <= 0 || it.TrimStart < 0 {
		return opErrf(i, opName, ErrOutOfRange, "item %q has invalid geometry (from=%d dur=%d trimStart=%d)", it.ItemID, it.From, it.DurationInFrames, it.TrimStart)
	}
	if math.IsNaN(it.Speed) || math.IsInf(it.Speed, 0) ||
		it.Speed < 0 || (it.Speed != 0 && (it.Speed < 0.1 || it.Speed > 10)) {
		return opErrf(i, opName, ErrBadRequest, "speed must be within [0.1, 10]")
	}
	if math.IsNaN(it.Volume) || math.IsInf(it.Volume, 0) || it.Volume < 0 || it.Volume > 2 {
		return opErrf(i, opName, ErrBadRequest, "volume must be within [0, 2]")
	}
	if it.FadeInFrames < 0 || it.FadeOutFrames < 0 {
		return opErrf(i, opName, ErrBadRequest, "fadeInFrames and fadeOutFrames must be >= 0")
	}
	if it.FadeInFrames+it.FadeOutFrames > it.DurationInFrames {
		return opErrf(i, opName, ErrOutOfRange,
			"fadeInFrames + fadeOutFrames must not exceed durationInFrames")
	}
	if it.Muted || it.FadeInFrames != 0 || it.FadeOutFrames != 0 {
		if it.Kind != ItemVideo && it.Kind != ItemAudio {
			return opErrf(i, opName, ErrBadRequest, "muted and fades only apply to video/audio items")
		}
	}
	if err := validateItemTransform(i, opName, it); err != nil {
		return err
	}
	if it.Kind == ItemText {
		if it.MediaID != "" {
			return opErrf(i, opName, ErrBadRequest, "text item must not reference media")
		}
		if strings.TrimSpace(it.Text) == "" {
			return opErrf(i, opName, ErrBadRequest, "text item requires non-empty text")
		}
		if err := validateCaptionStyle(i, opName, it.CaptionStyle); err != nil {
			return err
		}
		return nil
	}
	if it.CaptionStyle != nil {
		return opErrf(i, opName, ErrInvalidCaptionStyle, "captionStyle only applies to text items")
	}
	// media item
	wantKind, _ := mediaKindForItem(it.Kind)
	mi, ok := d.mediaByID(it.MediaID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "media %q not found", it.MediaID)
	}
	m := d.Media[mi]
	if m.Kind != wantKind {
		return opErrf(i, opName, ErrKindMismatch, "item kind %s requires %s media, got %s", it.Kind, wantKind, m.Kind)
	}
	// 图片无源长限制；视频/音频要求消耗的源区间不越界。
	if m.Kind != MediaImage {
		sourceEnd := float64(it.TrimStart) + float64(it.DurationInFrames)*it.EffectiveSpeed()
		if sourceEnd > float64(m.DurationInFrames)+1e-9 {
			return opErrf(i, opName, ErrOutOfRange,
				"item %q consumes source frames [%d, %.9f) beyond media duration %d",
				it.ItemID, it.TrimStart, sourceEnd, m.DurationInFrames)
		}
	}
	return nil
}

func validateItemTransform(i int, opName string, it Item) *OpError {
	if it.Transform == nil {
		return nil
	}
	if it.Kind != ItemVideo && it.Kind != ItemImage {
		return opErrf(i, opName, ErrBadRequest, "transform only applies to video/image items")
	}
	transform := it.Transform
	values := []float64{
		transform.X, transform.Y, transform.Width, transform.Height, transform.Opacity,
		transform.RotationDegrees,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return opErrf(i, opName, ErrBadRequest, "transform values must be finite")
		}
	}
	if transform.X < 0 || transform.Y < 0 || transform.Width <= 0 || transform.Height <= 0 ||
		transform.X > 1 || transform.Y > 1 || transform.Width > 1 || transform.Height > 1 ||
		transform.X+transform.Width > 1 || transform.Y+transform.Height > 1 {
		return opErrf(i, opName, ErrOutOfRange, "transform rectangle must fit within normalized canvas bounds")
	}
	if transform.Opacity < 0 || transform.Opacity > 1 {
		return opErrf(i, opName, ErrOutOfRange, "transform opacity must be within [0, 1]")
	}
	if transform.Fit != "" && transform.Fit != ItemFitContain && transform.Fit != ItemFitCover {
		return opErrf(i, opName, ErrBadRequest, "transform fit must be contain or cover")
	}
	if transform.RotationDegrees < -180 || transform.RotationDegrees > 180 {
		return opErrf(i, opName, ErrOutOfRange, "rotationDegrees must be within [-180, 180]")
	}
	if transform.Crop != nil {
		crop := transform.Crop
		values := []float64{crop.X, crop.Y, crop.Width, crop.Height}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return opErrf(i, opName, ErrBadRequest, "crop values must be finite")
			}
		}
		if crop.X < 0 || crop.Y < 0 || crop.Width <= 0 || crop.Height <= 0 ||
			crop.X > 1 || crop.Y > 1 || crop.Width > 1 || crop.Height > 1 ||
			crop.X+crop.Width > 1+1e-12 || crop.Y+crop.Height > 1+1e-12 {
			return opErrf(i, opName, ErrOutOfRange, "crop rectangle must fit within normalized source bounds")
		}
	}
	return nil
}

func validHexColor(value string, bytes int) bool {
	if len(value) != 1+bytes*2 || value[0] != '#' {
		return false
	}
	_, err := hex.DecodeString(value[1:])
	return err == nil
}

func validateCaptionStyle(i int, opName string, style *CaptionStyle) *OpError {
	if style == nil {
		return nil
	}
	if style.FontFamily != nil {
		switch *style.FontFamily {
		case "default", "sans", "serif", "mono":
		default:
			return opErrf(i, opName, ErrInvalidCaptionStyle, "fontFamily must be default, sans, serif, or mono")
		}
	}
	checkFiniteRange := func(name string, value *float64, minValue, maxValue float64) *OpError {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < minValue || *value > maxValue) {
			return opErrf(i, opName, ErrInvalidCaptionStyle, "%s must be finite and within [%g, %g]", name, minValue, maxValue)
		}
		return nil
	}
	if err := checkFiniteRange("fontSize", style.FontSize, 8, 200); err != nil {
		return err
	}
	if style.TextColor != nil && !validHexColor(*style.TextColor, 3) {
		return opErrf(i, opName, ErrInvalidCaptionStyle, "textColor must be #RRGGBB")
	}
	if style.BackgroundColor != nil && *style.BackgroundColor != "" && !validHexColor(*style.BackgroundColor, 4) {
		return opErrf(i, opName, ErrInvalidCaptionStyle, "backgroundColor must be empty or #RRGGBBAA")
	}
	if style.OutlineColor != nil && !validHexColor(*style.OutlineColor, 3) {
		return opErrf(i, opName, ErrInvalidCaptionStyle, "outlineColor must be #RRGGBB")
	}
	if err := checkFiniteRange("outlineWidth", style.OutlineWidth, 0, 8); err != nil {
		return err
	}
	if style.Align != nil {
		switch *style.Align {
		case "left", "center", "right":
		default:
			return opErrf(i, opName, ErrInvalidCaptionStyle, "align must be left, center, or right")
		}
	}
	if err := checkFiniteRange("x", style.X, 0, 1); err != nil {
		return err
	}
	if err := checkFiniteRange("y", style.Y, 0, 1); err != nil {
		return err
	}
	if err := checkFiniteRange("maxWidth", style.MaxWidth, 0.1, 1); err != nil {
		return err
	}
	if err := checkFiniteRange("lineHeight", style.LineHeight, 0.8, 3); err != nil {
		return err
	}
	return nil
}

func mergeCaptionStyle(current, patch *CaptionStyle) *CaptionStyle {
	if patch == nil {
		return cloneCaptionStyle(current)
	}
	if captionStyleEmpty(patch) {
		return nil
	}
	out := cloneCaptionStyle(current)
	if out == nil {
		out = &CaptionStyle{}
	}
	if patch.FontFamily != nil {
		value := *patch.FontFamily
		out.FontFamily = &value
	}
	if patch.FontSize != nil {
		value := *patch.FontSize
		out.FontSize = &value
	}
	if patch.TextColor != nil {
		value := *patch.TextColor
		out.TextColor = &value
	}
	if patch.BackgroundColor != nil {
		value := *patch.BackgroundColor
		out.BackgroundColor = &value
	}
	if patch.OutlineColor != nil {
		value := *patch.OutlineColor
		out.OutlineColor = &value
	}
	if patch.OutlineWidth != nil {
		value := *patch.OutlineWidth
		out.OutlineWidth = &value
	}
	if patch.Bold != nil {
		value := *patch.Bold
		out.Bold = &value
	}
	if patch.Italic != nil {
		value := *patch.Italic
		out.Italic = &value
	}
	if patch.Align != nil {
		value := *patch.Align
		out.Align = &value
	}
	if patch.X != nil {
		value := *patch.X
		out.X = &value
	}
	if patch.Y != nil {
		value := *patch.Y
		out.Y = &value
	}
	if patch.MaxWidth != nil {
		value := *patch.MaxWidth
		out.MaxWidth = &value
	}
	if patch.LineHeight != nil {
		value := *patch.LineHeight
		out.LineHeight = &value
	}
	return out
}

func captionStyleEmpty(style *CaptionStyle) bool {
	return style == nil || (style.FontFamily == nil && style.FontSize == nil &&
		style.TextColor == nil && style.BackgroundColor == nil &&
		style.OutlineColor == nil && style.OutlineWidth == nil &&
		style.Bold == nil && style.Italic == nil && style.Align == nil &&
		style.X == nil && style.Y == nil && style.MaxWidth == nil &&
		style.LineHeight == nil)
}

func clampItemFades(it *Item) {
	if it.FadeInFrames > it.DurationInFrames {
		it.FadeInFrames = it.DurationInFrames
	}
	remaining := it.DurationInFrames - it.FadeInFrames
	if it.FadeOutFrames > remaining {
		it.FadeOutFrames = remaining
	}
}

func trimItemFadeStart(it *Item, removed int64) {
	if removed > 0 {
		it.FadeInFrames = max(int64(0), it.FadeInFrames-removed)
	}
	clampItemFades(it)
}

func trimItemFadeEnd(it *Item, removed int64) {
	if removed > 0 {
		it.FadeOutFrames = max(int64(0), it.FadeOutFrames-removed)
	}
	clampItemFades(it)
}

func applyAddItem(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Item == nil {
		return opErrf(i, op.Op, ErrBadRequest, "item is required")
	}
	it := cloneItem(*op.Item)
	if _, exists := d.itemByID(it.ItemID); exists {
		return opErrf(i, op.Op, ErrConflictingID, "item %q already exists", it.ItemID)
	}
	if _, err := requireUnlockedTrack(d, i, op.Op, it.TrackID); err != nil {
		return err
	}
	if err := validateItemShape(d, i, op.Op, it); err != nil {
		return err
	}
	if len(d.Items) >= MaxItems {
		return opErrf(i, op.Op, ErrLimitExceeded, "item count would exceed %d", MaxItems)
	}
	d.Items = append(d.Items, it)
	rec.addedItems = append(rec.addedItems, it.ItemID)
	return nil
}

func applyRemoveItems(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if len(op.ItemIDs) == 0 {
		return opErrf(i, op.Op, ErrBadRequest, "itemIds is required")
	}
	// 先全部校验再删除，保证单 op 原子性。
	for _, id := range op.ItemIDs {
		if _, err := requireUnlockedItem(d, i, op.Op, id); err != nil {
			return err
		}
	}
	remove := make(map[string]bool, len(op.ItemIDs))
	for _, id := range op.ItemIDs {
		remove[id] = true
	}
	kept := d.Items[:0]
	for _, it := range d.Items {
		if !remove[it.ItemID] {
			kept = append(kept, it)
		}
	}
	d.Items = kept
	rec.removedItems = append(rec.removedItems, op.ItemIDs...)
	removeTransitionsForItems(d, rec, remove)
	return nil
}

// deriveSplitID 为 split 右半段派生确定性 ID（Workflow 内禁止随机数）。
func deriveSplitID(itemID string, frame int64) string {
	return fmt.Sprintf("%s~%d", itemID, frame)
}

func applySplit(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Frame == nil {
		return opErrf(i, op.Op, ErrBadRequest, "frame is required")
	}
	ii, err := requireUnlockedItem(d, i, op.Op, op.ItemID)
	if err != nil {
		return err
	}
	it := d.Items[ii]
	frame := *op.Frame
	if frame <= it.From || frame >= it.End() {
		return opErrf(i, op.Op, ErrOutOfRange, "split frame %d must be strictly inside item [%d, %d)", frame, it.From, it.End())
	}
	newID := op.NewItemID
	if newID == "" {
		newID = deriveSplitID(it.ItemID, frame)
	}
	if _, exists := d.itemByID(newID); exists {
		return opErrf(i, op.Op, ErrConflictingID, "item %q already exists", newID)
	}
	if len(d.Items) >= MaxItems {
		return opErrf(i, op.Op, ErrLimitExceeded, "item count would exceed %d", MaxItems)
	}

	leftDur := frame - it.From
	sourceDelta, exact := exactSourceFrameDelta(leftDur, it.EffectiveSpeed())
	if (it.Kind == ItemVideo || it.Kind == ItemAudio) && !exact {
		return opErrf(i, op.Op, ErrOutOfRange,
			"split frame maps to a fractional source frame; choose a frame aligned to item speed")
	}
	right := cloneItem(it)
	right.ItemID = newID
	right.From = frame
	right.DurationInFrames = it.End() - frame
	right.FadeInFrames = 0
	// 右半段的源起点 = 原 trimStart + 左半段消耗的源帧数（源区间总量守恒）。
	right.TrimStart = it.TrimStart + sourceDelta

	d.Items[ii].DurationInFrames = leftDur
	d.Items[ii].FadeOutFrames = 0
	clampItemFades(&d.Items[ii])
	clampItemFades(&right)
	d.Items = append(d.Items, right)
	rec.updatedItems = append(rec.updatedItems, it.ItemID)
	rec.addedItems = append(rec.addedItems, newID)
	return nil
}

// applyTrim 处理 trimStart（isStart=true，改左沿）与 trimEnd（改右沿，开区间）。
// frame 是时间线绝对帧，不是 delta。
func applyTrim(d *Doc, i int, op Operation, rec *recorder, isStart bool) *OpError {
	if op.Frame == nil {
		return opErrf(i, op.Op, ErrBadRequest, "frame is required")
	}
	ii, err := requireUnlockedItem(d, i, op.Op, op.ItemID)
	if err != nil {
		return err
	}
	it := &d.Items[ii]
	frame := *op.Frame
	oldFrom, oldEnd := it.From, it.End()
	if isStart {
		if frame < 0 || frame >= it.End() {
			return opErrf(i, op.Op, ErrOutOfRange, "trimStart frame %d must be within [0, %d)", frame, it.End())
		}
		delta := frame - it.From // 正 = 收窄，负 = 向左扩展
		sourceDelta, exact := exactSourceFrameDelta(delta, it.EffectiveSpeed())
		if (it.Kind == ItemVideo || it.Kind == ItemAudio) && !exact {
			return opErrf(i, op.Op, ErrOutOfRange,
				"trimStart frame maps to a fractional source frame; choose a frame aligned to item speed")
		}
		newTrim := it.TrimStart + sourceDelta
		if it.Kind == ItemVideo || it.Kind == ItemAudio {
			if newTrim < 0 {
				return opErrf(i, op.Op, ErrOutOfRange, "cannot extend item %q before source start", it.ItemID)
			}
			it.TrimStart = newTrim
		}
		it.DurationInFrames = it.End() - frame
		it.From = frame
		trimItemFadeStart(it, frame-oldFrom)
	} else {
		if frame <= it.From {
			return opErrf(i, op.Op, ErrOutOfRange, "trimEnd frame %d must be > item start %d", frame, it.From)
		}
		it.DurationInFrames = frame - it.From
		if it.Kind == ItemVideo || it.Kind == ItemAudio {
			mi, ok := d.mediaByID(it.MediaID)
			sourceEnd := float64(it.TrimStart) + float64(it.DurationInFrames)*it.EffectiveSpeed()
			if ok && sourceEnd > float64(d.Media[mi].DurationInFrames)+1e-9 {
				return opErrf(i, op.Op, ErrOutOfRange, "cannot extend item %q beyond source end", it.ItemID)
			}
		}
		trimItemFadeEnd(it, oldEnd-frame)
	}
	rec.updatedItems = append(rec.updatedItems, it.ItemID)
	return nil
}

func applyMoveItem(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.From == nil {
		return opErrf(i, op.Op, ErrBadRequest, "from is required")
	}
	ii, err := requireUnlockedItem(d, i, op.Op, op.ItemID)
	if err != nil {
		return err
	}
	it := &d.Items[ii]
	if *op.From < 0 {
		return opErrf(i, op.Op, ErrOutOfRange, "from must be >= 0")
	}
	if op.TrackID != "" && op.TrackID != it.TrackID {
		ti, terr := requireUnlockedTrack(d, i, op.Op, op.TrackID)
		if terr != nil {
			return terr
		}
		if !itemKindAllowedOn(it.Kind, d.Tracks[ti].Kind) {
			return opErrf(i, op.Op, ErrKindMismatch, "%s item not allowed on %s track", it.Kind, d.Tracks[ti].Kind)
		}
		it.TrackID = op.TrackID
	}
	it.From = *op.From
	rec.updatedItems = append(rec.updatedItems, it.ItemID)
	return nil
}

func applyUpdateItem(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Patch == nil {
		return opErrf(i, op.Op, ErrBadRequest, "patch is required")
	}
	ii, err := requireUnlockedItem(d, i, op.Op, op.ItemID)
	if err != nil {
		return err
	}
	it := &d.Items[ii]
	p := op.Patch
	if p.Speed != nil {
		if math.IsNaN(*p.Speed) || math.IsInf(*p.Speed, 0) || *p.Speed < 0.1 || *p.Speed > 10 {
			return opErrf(i, op.Op, ErrBadRequest, "speed must be within [0.1, 10]")
		}
		if it.Kind == ItemText || it.Kind == ItemImage {
			return opErrf(i, op.Op, ErrBadRequest, "speed only applies to video/audio items")
		}
		it.Speed = *p.Speed
		// 变速保持时间线时长不变，改变源消耗量；越界在此拦截。
		if mi, ok := d.mediaByID(it.MediaID); ok {
			sourceEnd := float64(it.TrimStart) + float64(it.DurationInFrames)*it.EffectiveSpeed()
			if sourceEnd > float64(d.Media[mi].DurationInFrames)+1e-9 {
				return opErrf(i, op.Op, ErrOutOfRange, "new speed consumes source beyond media duration; trim the item first")
			}
		}
	}
	if p.Volume != nil {
		if math.IsNaN(*p.Volume) || math.IsInf(*p.Volume, 0) || *p.Volume < 0 || *p.Volume > 2 {
			return opErrf(i, op.Op, ErrBadRequest, "volume must be within [0, 2]")
		}
		if it.Kind == ItemText || it.Kind == ItemImage {
			return opErrf(i, op.Op, ErrBadRequest, "volume only applies to video/audio items")
		}
		it.Volume = *p.Volume
	}
	if p.Muted != nil {
		if it.Kind != ItemVideo && it.Kind != ItemAudio {
			return opErrf(i, op.Op, ErrBadRequest, "muted only applies to video/audio items")
		}
		it.Muted = *p.Muted
	}
	if p.FadeInFrames != nil {
		it.FadeInFrames = *p.FadeInFrames
	}
	if p.FadeOutFrames != nil {
		it.FadeOutFrames = *p.FadeOutFrames
	}
	if p.Transform != nil {
		transform := *p.Transform
		if p.Transform.Crop != nil {
			crop := *p.Transform.Crop
			transform.Crop = &crop
		}
		it.Transform = &transform
	}
	if p.Text != nil {
		if it.Kind != ItemText {
			return opErrf(i, op.Op, ErrBadRequest, "text only applies to text items")
		}
		if strings.TrimSpace(*p.Text) == "" {
			return opErrf(i, op.Op, ErrBadRequest, "text must be non-empty")
		}
		it.Text = *p.Text
	}
	if p.StylePresetID != nil {
		it.StylePresetID = *p.StylePresetID
	}
	if p.CaptionStyle != nil {
		if it.Kind != ItemText {
			return opErrf(i, op.Op, ErrInvalidCaptionStyle, "captionStyle only applies to text items")
		}
		it.CaptionStyle = mergeCaptionStyle(it.CaptionStyle, p.CaptionStyle)
	}
	if shapeErr := validateItemShape(d, i, op.Op, *it); shapeErr != nil {
		return shapeErr
	}
	rec.updatedItems = append(rec.updatedItems, it.ItemID)
	return nil
}

func applyRippleDelete(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.FromFrame == nil || op.ToFrame == nil {
		return opErrf(i, op.Op, ErrBadRequest, "fromFrame and toFrame are required")
	}
	from, to := *op.FromFrame, *op.ToFrame
	if from < 0 || to <= from {
		return opErrf(i, op.Op, ErrOutOfRange, "invalid range [%d, %d)", from, to)
	}
	delta := to - from

	// 确定作用轨道：显式指定必须全部存在且未锁定；缺省 = 所有未锁定轨。
	affected := make(map[string]bool)
	if len(op.TrackIDs) > 0 {
		for _, tid := range op.TrackIDs {
			if _, err := requireUnlockedTrack(d, i, op.Op, tid); err != nil {
				return err
			}
			affected[tid] = true
		}
	} else {
		for _, t := range d.Tracks {
			if !t.Locked {
				affected[t.TrackID] = true
			}
		}
	}

	var out []Item
	for _, it := range d.Items {
		if !affected[it.TrackID] {
			out = append(out, it)
			continue
		}
		start, end := it.From, it.End()
		switch {
		case end <= from: // 完全在区间前，不动
			out = append(out, it)
		case start >= to: // 完全在区间后，整体左移
			it.From -= delta
			out = append(out, it)
			rec.updatedItems = append(rec.updatedItems, it.ItemID)
		case start >= from && end <= to: // 完全被删
			rec.removedItems = append(rec.removedItems, it.ItemID)
		case start < from && end > to: // 跨越整个区间：裂成左右两段
			sourceDelta, exact := exactSourceFrameDelta(to-start, it.EffectiveSpeed())
			if (it.Kind == ItemVideo || it.Kind == ItemAudio) && !exact {
				return opErrf(i, op.Op, ErrOutOfRange,
					"rippleDelete boundary maps to a fractional source frame for item %q", it.ItemID)
			}
			left := it
			left.DurationInFrames = from - start
			left.FadeOutFrames = 0
			clampItemFades(&left)
			right := cloneItem(it)
			right.ItemID = deriveSplitID(it.ItemID, to)
			if _, exists := d.itemByID(right.ItemID); exists {
				return opErrf(i, op.Op, ErrConflictingID, "derived item id %q already exists", right.ItemID)
			}
			right.From = from // 右段起点左移 delta（to - delta = from）
			right.DurationInFrames = end - to
			right.FadeInFrames = 0
			right.TrimStart = it.TrimStart + sourceDelta
			clampItemFades(&right)
			out = append(out, left, right)
			rec.updatedItems = append(rec.updatedItems, left.ItemID)
			rec.addedItems = append(rec.addedItems, right.ItemID)
		case start < from: // 尾部落入区间：截断到 from
			it.DurationInFrames = from - start
			trimItemFadeEnd(&it, end-from)
			out = append(out, it)
			rec.updatedItems = append(rec.updatedItems, it.ItemID)
		default: // 头部落入区间：裁掉 [start, to)，剩余部分左移到 from
			cut := to - start
			sourceDelta, exact := exactSourceFrameDelta(cut, it.EffectiveSpeed())
			if (it.Kind == ItemVideo || it.Kind == ItemAudio) && !exact {
				return opErrf(i, op.Op, ErrOutOfRange,
					"rippleDelete boundary maps to a fractional source frame for item %q", it.ItemID)
			}
			it.TrimStart += sourceDelta
			it.DurationInFrames = end - to
			it.From = from
			trimItemFadeStart(&it, cut)
			out = append(out, it)
			rec.updatedItems = append(rec.updatedItems, it.ItemID)
		}
	}
	if out == nil {
		out = []Item{}
	}
	d.Items = out
	return nil
}

// ---- tracks ----

func cloneDuckingConfig(config *DuckingConfig) *DuckingConfig {
	if config == nil {
		return nil
	}
	out := *config
	out.SidechainTrackIDs = append([]string(nil), config.SidechainTrackIDs...)
	return &out
}

func validateDuckingConfig(d *Doc, i int, opName string, track Track) *OpError {
	config := track.Ducking
	if config == nil {
		return nil
	}
	if track.Kind != TrackAudio {
		return opErrf(i, opName, ErrInvalidDucking, "ducking only applies to audio tracks")
	}
	if len(config.SidechainTrackIDs) == 0 {
		return opErrf(i, opName, ErrInvalidDucking, "sidechainTrackIds must not be empty")
	}
	if math.IsNaN(config.Gain) || math.IsInf(config.Gain, 0) || config.Gain <= 0 || config.Gain > 1 {
		return opErrf(i, opName, ErrInvalidDucking, "ducking gain must be finite and within (0, 1]")
	}
	maxEnvelopeFrames := int64(d.Metadata.FPS * 5)
	if config.AttackFrames < 0 || config.ReleaseFrames < 0 ||
		config.AttackFrames > maxEnvelopeFrames || config.ReleaseFrames > maxEnvelopeFrames {
		return opErrf(i, opName, ErrInvalidDucking,
			"attackFrames and releaseFrames must be within [0, %d]", maxEnvelopeFrames)
	}
	seen := make(map[string]bool, len(config.SidechainTrackIDs))
	for _, trackID := range config.SidechainTrackIDs {
		if trackID == track.TrackID {
			return opErrf(i, opName, ErrInvalidDucking, "track %q cannot duck from itself", track.TrackID)
		}
		if seen[trackID] {
			return opErrf(i, opName, ErrInvalidDucking, "duplicate sidechain track %q", trackID)
		}
		seen[trackID] = true
		ti, ok := d.trackByID(trackID)
		if !ok {
			return opErrf(i, opName, ErrInvalidDucking, "sidechain track %q not found", trackID)
		}
		if d.Tracks[ti].Kind != TrackAudio && d.Tracks[ti].Kind != TrackVideo {
			return opErrf(i, opName, ErrInvalidDucking, "sidechain track %q must be audio or video", trackID)
		}
	}
	return nil
}

func applyAddTrack(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Track == nil {
		return opErrf(i, op.Op, ErrBadRequest, "track is required")
	}
	t := *op.Track
	t.Ducking = cloneDuckingConfig(op.Track.Ducking)
	if t.TrackID == "" {
		return opErrf(i, op.Op, ErrBadRequest, "trackId is required")
	}
	switch t.Kind {
	case TrackVideo, TrackAudio, TrackCaption:
	default:
		return opErrf(i, op.Op, ErrBadRequest, "unknown track kind %q", t.Kind)
	}
	if t.Hidden && t.Kind != TrackVideo {
		return opErrf(i, op.Op, ErrBadRequest, "hidden only applies to video tracks")
	}
	if _, exists := d.trackByID(t.TrackID); exists {
		return opErrf(i, op.Op, ErrConflictingID, "track %q already exists", t.TrackID)
	}
	if math.IsNaN(t.Volume) || math.IsInf(t.Volume, 0) || t.Volume < 0 || t.Volume > 2 {
		return opErrf(i, op.Op, ErrBadRequest, "track volume must be within [0, 2]")
	}
	if err := validateDuckingConfig(d, i, op.Op, t); err != nil {
		return err
	}
	if len(d.Tracks) >= MaxTracks {
		return opErrf(i, op.Op, ErrLimitExceeded, "track count would exceed %d", MaxTracks)
	}
	pos := len(d.Tracks)
	if op.Index != nil {
		if *op.Index < 0 || *op.Index > len(d.Tracks) {
			return opErrf(i, op.Op, ErrOutOfRange, "index %d out of range [0, %d]", *op.Index, len(d.Tracks))
		}
		pos = *op.Index
	}
	d.Tracks = append(d.Tracks, Track{})
	copy(d.Tracks[pos+1:], d.Tracks[pos:])
	d.Tracks[pos] = t
	rec.addedTracks = append(rec.addedTracks, t.TrackID)
	return nil
}

func applyRemoveTrack(d *Doc, i int, op Operation, rec *recorder) *OpError {
	ti, err := requireUnlockedTrack(d, i, op.Op, op.TrackID)
	if err != nil {
		return err
	}
	removedItemIDs := make(map[string]bool)
	kept := d.Items[:0]
	for _, it := range d.Items {
		if it.TrackID == op.TrackID {
			rec.removedItems = append(rec.removedItems, it.ItemID)
			removedItemIDs[it.ItemID] = true
		} else {
			kept = append(kept, it)
		}
	}
	d.Items = kept
	removeTransitionsForItems(d, rec, removedItemIDs)
	d.Tracks = append(d.Tracks[:ti], d.Tracks[ti+1:]...)
	for trackIndex := range d.Tracks {
		config := d.Tracks[trackIndex].Ducking
		if config == nil {
			continue
		}
		filtered := make([]string, 0, len(config.SidechainTrackIDs))
		changed := false
		for _, sidechainID := range config.SidechainTrackIDs {
			if sidechainID == op.TrackID {
				changed = true
				continue
			}
			filtered = append(filtered, sidechainID)
		}
		if !changed {
			continue
		}
		if len(filtered) == 0 {
			d.Tracks[trackIndex].Ducking = nil
		} else {
			updated := *config
			updated.SidechainTrackIDs = filtered
			d.Tracks[trackIndex].Ducking = &updated
		}
		rec.updatedTracks = append(rec.updatedTracks, d.Tracks[trackIndex].TrackID)
	}
	rec.removedTracks = append(rec.removedTracks, op.TrackID)
	return nil
}

func applyUpdateTrack(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.TrackPatch == nil {
		return opErrf(i, op.Op, ErrBadRequest, "trackPatch is required")
	}
	ti, ok := d.trackByID(op.TrackID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "track %q not found", op.TrackID)
	}
	t := &d.Tracks[ti]
	p := op.TrackPatch
	// 锁定轨上唯一允许的修改是 locked 字段本身（否则无法解锁）；
	// 其余字段必须先解锁（可在同批内先发一个解锁 patch）。
	if t.Locked && (p.Name != nil || p.Muted != nil || p.Hidden != nil || p.Volume != nil || p.Ducking != nil) {
		return opErrf(i, op.Op, ErrLockedTrack, "track %q is locked; unlock it before changing other fields", op.TrackID)
	}
	if p.Hidden != nil && t.Kind != TrackVideo {
		return opErrf(i, op.Op, ErrBadRequest, "hidden only applies to video tracks")
	}
	if p.Volume != nil {
		if math.IsNaN(*p.Volume) || math.IsInf(*p.Volume, 0) || *p.Volume < 0 || *p.Volume > 2 {
			return opErrf(i, op.Op, ErrBadRequest, "track volume must be within [0, 2]")
		}
		t.Volume = *p.Volume
	}
	if p.Name != nil {
		t.Name = *p.Name
	}
	if p.Locked != nil {
		t.Locked = *p.Locked
	}
	if p.Muted != nil {
		t.Muted = *p.Muted
	}
	if p.Hidden != nil {
		t.Hidden = *p.Hidden
	}
	if p.Ducking != nil {
		if len(p.Ducking.SidechainTrackIDs) == 0 {
			t.Ducking = nil
			rec.updatedTracks = append(rec.updatedTracks, t.TrackID)
			return nil
		}
		candidate := *t
		candidate.Ducking = cloneDuckingConfig(p.Ducking)
		if err := validateDuckingConfig(d, i, op.Op, candidate); err != nil {
			return err
		}
		t.Ducking = candidate.Ducking
	}
	rec.updatedTracks = append(rec.updatedTracks, t.TrackID)
	return nil
}

// ---- transitions ----

func visualItem(kind ItemKind) bool {
	return kind == ItemVideo || kind == ItemImage
}

func validateTransitionShape(d *Doc, i int, opName string, transition Transition, excludeIndex int) *OpError {
	if !validTransitionID(transition.TransitionID) {
		return opErrf(i, opName, ErrInvalidTransition,
			"transitionId must match [A-Za-z0-9_-]{1,%d}", MaxTransitionIDLength)
	}
	switch transition.Kind {
	case TransitionCrossfade, TransitionDipToBlack:
	default:
		return opErrf(i, opName, ErrInvalidTransition, "transition kind must be crossfade or dipToBlack")
	}
	if transition.FromItemID == transition.ToItemID {
		return opErrf(i, opName, ErrInvalidTransition, "transition items must be different")
	}
	fromIndex, ok := d.itemByID(transition.FromItemID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "from item %q not found", transition.FromItemID)
	}
	toIndex, ok := d.itemByID(transition.ToItemID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "to item %q not found", transition.ToItemID)
	}
	fromItem, toItem := d.Items[fromIndex], d.Items[toIndex]
	if !visualItem(fromItem.Kind) || !visualItem(toItem.Kind) {
		return opErrf(i, opName, ErrInvalidTransition, "transitions require video or image items")
	}
	if fromItem.TrackID != toItem.TrackID {
		return opErrf(i, opName, ErrInvalidTransition, "transition items must be on the same track")
	}
	trackIndex, ok := d.trackByID(fromItem.TrackID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "track %q not found", fromItem.TrackID)
	}
	if d.Tracks[trackIndex].Kind != TrackVideo {
		return opErrf(i, opName, ErrInvalidTransition, "transitions require a video track")
	}
	if fromItem.End() != toItem.From {
		return opErrf(i, opName, ErrInvalidTransition,
			"items %q and %q must be exactly adjacent", fromItem.ItemID, toItem.ItemID)
	}
	if transition.Frames <= 0 || transition.Frames > toItem.DurationInFrames {
		return opErrf(i, opName, ErrInvalidTransition,
			"frames must be positive and no greater than to item duration %d", toItem.DurationInFrames)
	}
	for index, existing := range d.Transitions {
		if index == excludeIndex {
			continue
		}
		if existing.FromItemID == transition.FromItemID && existing.ToItemID == transition.ToItemID {
			return opErrf(i, opName, ErrInvalidTransition,
				"boundary %q -> %q already has transition %q",
				transition.FromItemID, transition.ToItemID, existing.TransitionID)
		}
	}
	if fromItem.Kind == ItemVideo {
		mediaIndex, ok := d.mediaByID(fromItem.MediaID)
		if !ok {
			return opErrf(i, opName, ErrNotFound, "media %q not found", fromItem.MediaID)
		}
		speed := fromItem.EffectiveSpeed()
		sourceEnd := float64(fromItem.TrimStart) + float64(fromItem.DurationInFrames)*speed
		available := float64(d.Media[mediaIndex].DurationInFrames) - sourceEnd
		required := float64(transition.Frames) * speed
		if available+1e-9 < required {
			return opErrf(i, opName, ErrInsufficientHandle,
				"from item %q has %.9f source frames of handle, needs %.9f",
				fromItem.ItemID, available, required)
		}
	}
	return nil
}

func validTransitionID(id string) bool {
	if len(id) == 0 || len(id) > MaxTransitionIDLength {
		return false
	}
	for _, c := range []byte(id) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func requireUnlockedTransitionTrack(d *Doc, i int, opName string, transition Transition) *OpError {
	fromIndex, ok := d.itemByID(transition.FromItemID)
	if !ok {
		return opErrf(i, opName, ErrNotFound, "from item %q not found", transition.FromItemID)
	}
	_, err := requireUnlockedTrack(d, i, opName, d.Items[fromIndex].TrackID)
	return err
}

func applyAddTransition(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Transition == nil {
		return opErrf(i, op.Op, ErrBadRequest, "transition is required")
	}
	transition := *op.Transition
	if _, exists := d.transitionByID(transition.TransitionID); exists {
		return opErrf(i, op.Op, ErrConflictingID, "transition %q already exists", transition.TransitionID)
	}
	if len(d.Transitions) >= MaxTransitions {
		return opErrf(i, op.Op, ErrLimitExceeded, "transition count would exceed %d", MaxTransitions)
	}
	if err := validateTransitionShape(d, i, op.Op, transition, -1); err != nil {
		return err
	}
	if err := requireUnlockedTransitionTrack(d, i, op.Op, transition); err != nil {
		return err
	}
	d.Transitions = append(d.Transitions, transition)
	rec.addedTransitions = append(rec.addedTransitions, transition.TransitionID)
	return nil
}

func applyUpdateTransition(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.TransitionPatch == nil {
		return opErrf(i, op.Op, ErrBadRequest, "transitionPatch is required")
	}
	if op.TransitionPatch.Kind == nil && op.TransitionPatch.Frames == nil {
		return opErrf(i, op.Op, ErrBadRequest, "transitionPatch must update at least one field")
	}
	transitionIndex, ok := d.transitionByID(op.TransitionID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "transition %q not found", op.TransitionID)
	}
	transition := d.Transitions[transitionIndex]
	if err := requireUnlockedTransitionTrack(d, i, op.Op, transition); err != nil {
		return err
	}
	if op.TransitionPatch.Kind != nil {
		transition.Kind = *op.TransitionPatch.Kind
	}
	if op.TransitionPatch.Frames != nil {
		transition.Frames = *op.TransitionPatch.Frames
	}
	if err := validateTransitionShape(d, i, op.Op, transition, transitionIndex); err != nil {
		return err
	}
	d.Transitions[transitionIndex] = transition
	rec.updatedTransitions = append(rec.updatedTransitions, transition.TransitionID)
	return nil
}

func applyRemoveTransition(d *Doc, i int, op Operation, rec *recorder) *OpError {
	transitionIndex, ok := d.transitionByID(op.TransitionID)
	if !ok {
		return opErrf(i, op.Op, ErrNotFound, "transition %q not found", op.TransitionID)
	}
	transition := d.Transitions[transitionIndex]
	if err := requireUnlockedTransitionTrack(d, i, op.Op, transition); err != nil {
		return err
	}
	d.Transitions = append(d.Transitions[:transitionIndex], d.Transitions[transitionIndex+1:]...)
	rec.removedTransitions = append(rec.removedTransitions, transition.TransitionID)
	return nil
}

func removeTransitionsForItems(d *Doc, rec *recorder, itemIDs map[string]bool) {
	if len(itemIDs) == 0 {
		return
	}
	kept := d.Transitions[:0]
	for _, transition := range d.Transitions {
		if itemIDs[transition.FromItemID] || itemIDs[transition.ToItemID] {
			rec.removedTransitions = append(rec.removedTransitions, transition.TransitionID)
			continue
		}
		kept = append(kept, transition)
	}
	d.Transitions = kept
}

func removeInvalidTransitions(d *Doc, rec *recorder) {
	validationDoc := *d
	validationDoc.Transitions = nil
	kept := d.Transitions[:0]
	for _, transition := range d.Transitions {
		if validateTransitionShape(&validationDoc, -1, "", transition, -1) != nil {
			rec.removedTransitions = append(rec.removedTransitions, transition.TransitionID)
			continue
		}
		kept = append(kept, transition)
	}
	d.Transitions = kept
}

// ---- captions / metadata ----

func applySetCaptions(d *Doc, i int, op Operation, rec *recorder) *OpError {
	ti, err := requireUnlockedTrack(d, i, op.Op, op.TrackID)
	if err != nil {
		return err
	}
	if d.Tracks[ti].Kind != TrackCaption {
		return opErrf(i, op.Op, ErrKindMismatch, "setCaptions requires a caption track, got %s", d.Tracks[ti].Kind)
	}
	// 先移除本轨全部旧字幕（整轨替换语义）。
	kept := d.Items[:0]
	for _, it := range d.Items {
		if it.TrackID == op.TrackID {
			rec.removedItems = append(rec.removedItems, it.ItemID)
		} else {
			kept = append(kept, it)
		}
	}
	d.Items = kept

	for ci, c := range op.Captions {
		if c.From < 0 || c.DurationInFrames <= 0 {
			return opErrf(i, op.Op, ErrOutOfRange, "caption[%d] has invalid geometry", ci)
		}
		if strings.TrimSpace(c.Text) == "" {
			return opErrf(i, op.Op, ErrBadRequest, "caption[%d] requires non-empty text", ci)
		}
		it := Item{
			ItemID:           fmt.Sprintf("%s_cap%d", op.TrackID, ci),
			TrackID:          op.TrackID,
			Kind:             ItemText,
			From:             c.From,
			DurationInFrames: c.DurationInFrames,
			Text:             c.Text,
			StylePresetID:    c.StylePresetID,
			CaptionStyle:     cloneCaptionStyle(c.CaptionStyle),
		}
		if _, exists := d.itemByID(it.ItemID); exists {
			return opErrf(i, op.Op, ErrConflictingID, "derived caption id %q already exists", it.ItemID)
		}
		if len(d.Items) >= MaxItems {
			return opErrf(i, op.Op, ErrLimitExceeded, "item count would exceed %d", MaxItems)
		}
		d.Items = append(d.Items, it)
		rec.addedItems = append(rec.addedItems, it.ItemID)
	}
	return nil
}

func applySetMetadata(d *Doc, i int, op Operation, rec *recorder) *OpError {
	if op.Metadata == nil {
		return opErrf(i, op.Op, ErrBadRequest, "metadata is required")
	}
	p := op.Metadata
	if p.FPS != nil && *p.FPS != d.Metadata.FPS {
		if !AllowedFPS[*p.FPS] {
			return opErrf(i, op.Op, ErrBadRequest, "fps %d not allowed", *p.FPS)
		}
		// 所有帧坐标（含 media.durationInFrames）都按当前 fps 表达，
		// 非空文档改 fps 会让全部坐标失真，MVP 一律禁止。
		if len(d.Items) > 0 || len(d.Media) > 0 {
			return opErrf(i, op.Op, ErrFPSLocked, "cannot change fps on a non-empty timeline")
		}
		d.Metadata.FPS = *p.FPS
	}
	if p.Width != nil {
		if *p.Width < 16 || *p.Width > 8192 {
			return opErrf(i, op.Op, ErrBadRequest, "width must be within [16, 8192]")
		}
		d.Metadata.Width = *p.Width
	}
	if p.Height != nil {
		if *p.Height < 16 || *p.Height > 8192 {
			return opErrf(i, op.Op, ErrBadRequest, "height must be within [16, 8192]")
		}
		d.Metadata.Height = *p.Height
	}
	if p.Name != nil {
		d.Metadata.Name = *p.Name
	}
	if p.BackgroundColor != nil {
		if err := ValidateBackgroundColor(*p.BackgroundColor); err != nil {
			return opErrf(i, op.Op, ErrBadRequest, "%v", err)
		}
		d.Metadata.BackgroundColor = *p.BackgroundColor
	}
	rec.metadataChanged = true
	return nil
}
