package render

import (
	"encoding/base64"
	"fmt"
	"sort"

	platformmedia "agentcut/platform/media"
	"agentcut/timeline"
)

// Compile 把 Doc 编译成 FfmpegTool 调用计划。纯函数、确定性。
func Compile(doc timeline.Doc) (Plan, error) {
	return compile(doc, false)
}

// CompileManaged emits internal media references for local rendering. The
// Activity resolves those references through Timeline BlobKey registrations;
// Timeline URI values are never passed to ffmpeg.
func CompileManaged(doc timeline.Doc) (Plan, error) {
	return compile(doc, true)
}

func compile(doc timeline.Doc, managedSources bool) (Plan, error) {
	background, err := renderBackground(doc)
	if err != nil {
		return Plan{}, err
	}
	if !managedSources {
		if err := rejectUnsupportedAgentKFeatures(doc); err != nil {
			return Plan{}, err
		}
	}
	c := &compiler{
		doc: doc, fps: doc.Metadata.FPS, managedSources: managedSources,
		background: background,
	}
	if c.fps <= 0 || doc.Metadata.Width <= 0 || doc.Metadata.Height <= 0 {
		return Plan{}, fmt.Errorf("render: doc metadata is incomplete (fps/width/height)")
	}
	total := timeline.DocDurationInFrames(doc)
	if total <= 0 {
		return Plan{}, fmt.Errorf("render: timeline is empty")
	}
	if total > int64(doc.Metadata.FPS*platformmedia.MaxMediaDurationSec) {
		return Plan{}, fmt.Errorf("render: timeline duration exceeds %d seconds", platformmedia.MaxMediaDurationSec)
	}
	// 轨道分类（保持 doc.Tracks 声明顺序 = 合成顺序）。
	var videoTracks, audioTracks, captionTracks []timeline.Track
	for _, tr := range doc.Tracks {
		switch tr.Kind {
		case timeline.TrackVideo:
			videoTracks = append(videoTracks, tr)
		case timeline.TrackAudio:
			audioTracks = append(audioTracks, tr)
		case timeline.TrackCaption:
			captionTracks = append(captionTracks, tr)
		}
	}
	visualVideoTracks := make([]timeline.Track, 0, len(videoTracks))
	for _, tr := range videoTracks {
		if !tr.Hidden {
			visualVideoTracks = append(visualVideoTracks, tr)
		}
	}

	// 1. 底层画面：第一条有内容的 video 轨。
	baseRef := ""
	baseIdx := -1
	for i, tr := range visualVideoTracks {
		if len(c.trackItems(tr.TrackID)) > 0 {
			var ref string
			var err error
			if managedSources && requiresCanvasComposition(doc, tr.TrackID) {
				ref, err = c.compileCanvasComposition(visualVideoTracks[i:], total)
				baseIdx = len(visualVideoTracks) - 1
			} else {
				ref, err = c.compileBaseTrack(tr, total)
				c.baseAudioTrackID = tr.TrackID
				baseIdx = i
			}
			if err != nil {
				return Plan{}, err
			}
			baseRef = ref
			break
		}
	}
	if baseRef == "" {
		hasHiddenVisualContent := false
		for _, tr := range videoTracks {
			if tr.Hidden && len(c.trackItems(tr.TrackID)) > 0 {
				hasHiddenVisualContent = true
				break
			}
		}
		if !hasHiddenVisualContent {
			return Plan{}, fmt.Errorf("render: no video track with items; MVP requires at least one visual item")
		}
		baseRef = c.emit("canvas:background", map[string]any{
			"op": "title_card",
			"params": map[string]any{
				"text": "", "bg": c.background,
				"w": doc.Metadata.Width, "h": doc.Metadata.Height,
				"fps": c.fps, "duration": c.sec(total),
			},
		})
	}

	// 2. 其余 video 轨逐片段叠加（声明顺序，后者盖前者）。
	for i, tr := range visualVideoTracks {
		if i <= baseIdx {
			continue
		}
		ref, err := c.compileOverlayTrack(tr, baseRef)
		if err != nil {
			return Plan{}, err
		}
		baseRef = ref
	}

	// 3. 混入 audio 轨。
	ref, err := c.compileAudioMix(audioTracks, baseRef)
	if err != nil {
		return Plan{}, err
	}
	baseRef = ref

	// 4. 烧字幕。
	baseRef, err = c.compileCaptions(captionTracks, baseRef)
	if err != nil {
		return Plan{}, err
	}
	_ = baseRef // 最后一个 call 的 artifact 即结果

	return Plan{Calls: c.calls, Warnings: c.warnings}, nil
}

// ---- compiler ----

type compiler struct {
	doc              timeline.Doc
	fps              int
	managedSources   bool
	background       string
	baseAudioTrackID string
	calls            []Call
	warnings         []string
}

func (c *compiler) warnf(format string, args ...any) {
	executor := "[KESS]"
	if c.managedSources {
		executor = "[local]"
	}
	c.warnings = append(c.warnings, executor+" "+fmt.Sprintf(format, args...))
}

func (c *compiler) warnKESSf(format string, args ...any) {
	if !c.managedSources {
		c.warnf(format, args...)
	}
}

// emit 追加一个调用并返回它的产物引用。
func (c *compiler) emit(id string, input map[string]any) string {
	c.calls = append(c.calls, Call{ID: id, Input: input})
	return OutputRef(id)
}

// trackItems 返回某轨道的片段，按 From、ItemID 排序（确定性）。
func (c *compiler) trackItems(trackID string) []timeline.Item {
	var items []timeline.Item
	for _, it := range c.doc.Items {
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
	return items
}

func (c *compiler) mediaURI(mediaID string) (string, error) {
	for _, m := range c.doc.Media {
		if m.MediaID == mediaID {
			if c.managedSources {
				return MediaRef(mediaID), nil
			}
			return m.URI, nil
		}
	}
	return "", fmt.Errorf("render: media %q not found", mediaID)
}

// sec converts frame counts with exact frame-rational semantics. Avoid
// independently rounding duration and fades: a legal fade sum equal to the
// item duration must remain legal at every supported fps.
func (c *compiler) sec(frames int64) float64 {
	return float64(frames) / float64(c.fps)
}

func (c *compiler) sourceSeconds(it timeline.Item) float64 {
	return float64(it.DurationInFrames) * it.EffectiveSpeed() / float64(c.fps)
}

// msOf 把帧数换成毫秒整数。
func (c *compiler) msOf(frames int64) int64 {
	return (frames*1000 + int64(c.fps)/2) / int64(c.fps)
}

// ---- 底层轨 ----

// compileBaseTrack 把一条 video 轨编译成覆盖 [0, total) 的整条视频。
func (c *compiler) compileBaseTrack(tr timeline.Track, total int64) (string, error) {
	items := c.trackItems(tr.TrackID)
	var segments []string
	cursor := int64(0)
	fillerN := 0

	for _, it := range items {
		if it.From > cursor {
			segments = append(segments, c.emitBlackFiller(tr.TrackID, fillerN, it.From-cursor))
			fillerN++
		}
		ref, err := c.emitItemPrep(tr, it, false)
		if err != nil {
			return "", err
		}
		segments = append(segments, ref)
		cursor = it.End()
	}
	if cursor < total {
		segments = append(segments, c.emitBlackFiller(tr.TrackID, fillerN, total-cursor))
	}

	if len(segments) == 1 {
		return segments[0], nil
	}
	params := map[string]any{
		"w": c.doc.Metadata.Width, "h": c.doc.Metadata.Height, "fps": c.fps,
	}
	if c.managedSources {
		params["bg"] = c.background
	}
	return c.emit("track:"+tr.TrackID+":concat", map[string]any{
		"op": "concat_av", "params": params,
		"videos": segments,
	}), nil
}

// compileCanvasComposition is used when the first visual track contains a
// transform. It ensures even the first track is composited over the canvas
// background instead of being treated as an implicit full-canvas base.
func (c *compiler) compileCanvasComposition(tracks []timeline.Track, total int64) (string, error) {
	baseRef := c.emit("canvas:background", map[string]any{
		"op": "title_card",
		"params": map[string]any{
			"text": "", "bg": c.background,
			"w": c.doc.Metadata.Width, "h": c.doc.Metadata.Height,
			"fps": c.fps, "duration": c.sec(total),
		},
	})
	for _, track := range tracks {
		var err error
		baseRef, err = c.compileOverlayTrack(track, baseRef)
		if err != nil {
			return "", err
		}
	}
	return baseRef, nil
}

// emitBlackFiller 用 title_card（无输入、纯色底）生成 n 帧画布背景补间隙。
func (c *compiler) emitBlackFiller(trackID string, n int, frames int64) string {
	c.warnKESSf("轨道 %s 的黑场间隙使用空文本 title_card，需在目标 KESS ffmpeg 服务验证", trackID)
	return c.emit(fmt.Sprintf("track:%s:gap%d", trackID, n), map[string]any{
		"op": "title_card",
		"params": map[string]any{
			"text": "", "bg": c.background,
			"w": c.doc.Metadata.Width, "h": c.doc.Metadata.Height,
			"fps": c.fps, "duration": c.sec(frames),
		},
	})
}

// emitItemPrep 把单个视觉片段预处理成时长精确的视频段。
func (c *compiler) emitItemPrep(tr timeline.Track, it timeline.Item, composite bool) (string, error) {
	id := "item:" + it.ItemID
	region := itemPixelRegion(it, c.doc.Metadata.Width, c.doc.Metadata.Height)
	prepWidth, prepHeight := encodedRegionDimensions(region)
	switch it.Kind {
	case timeline.ItemImage:
		uri, err := c.mediaURI(it.MediaID)
		if err != nil {
			return "", err
		}
		params := map[string]any{
			"duration": c.sec(it.DurationInFrames),
			"w":        prepWidth, "h": prepHeight,
			"fps": c.fps, "with_audio": true,
		}
		if c.managedSources {
			params["fit"] = region.Fit
			params["bg"] = c.background
			addVisualTransformParams(params, it)
		}
		return c.emit(id, map[string]any{
			"op": "freeze", "params": params,
			"image": uri,
		}), nil

	case timeline.ItemVideo:
		uri, err := c.mediaURI(it.MediaID)
		if err != nil {
			return "", err
		}
		clipParams := map[string]any{
			"start":    c.sec(it.TrimStart),
			"duration": c.sourceSeconds(it),
		}
		if c.managedSources {
			clipParams["w"], clipParams["h"] = prepWidth, prepHeight
			clipParams["fit"], clipParams["bg"] = region.Fit, c.background
			addVisualTransformParams(clipParams, it)
		}
		steps := []map[string]any{{
			"op":     "clip_av",
			"params": clipParams,
		}}
		if it.EffectiveSpeed() != 1 {
			params := map[string]any{"target_duration": c.sec(it.DurationInFrames)}
			if c.managedSources {
				params["w"], params["h"] = prepWidth, prepHeight
				params["fit"], params["bg"] = region.Fit, c.background
			}
			steps = append(steps, map[string]any{
				"op":     "retime_av",
				"params": params,
			})
		}
		muted := tr.Muted || it.Muted
		gain := effectiveVolume(tr.Volume) * effectiveVolume(it.Volume)
		if c.managedSources && (muted || gain != 1 || it.FadeInFrames != 0 || it.FadeOutFrames != 0) {
			steps = append(steps, map[string]any{
				"op": "apply_av_effects",
				"params": map[string]any{
					"duration": c.sec(it.DurationInFrames),
					"gain":     gain, "muted": muted,
					"fade_in": c.sec(it.FadeInFrames), "fade_out": c.sec(it.FadeOutFrames),
					"visual_fade": !composite,
					"w":           prepWidth, "h": prepHeight,
					"fit": region.Fit, "bg": c.background,
				},
			})
		} else if muted {
			steps = append(steps,
				map[string]any{"op": "strip_audio", "params": map[string]any{}},
				map[string]any{"op": "add_silent_audio", "params": map[string]any{}},
			)
		}
		return c.emit(id, map[string]any{"pipeline": steps, "video": uri}), nil

	default:
		return "", fmt.Errorf("render: item %q kind %q is not allowed on video track", it.ItemID, it.Kind)
	}
}

// ---- 叠加轨 ----

// compileOverlayTrack 把上层 video 轨的每个片段依次 overlay 到 base 上。
func (c *compiler) compileOverlayTrack(tr timeline.Track, baseRef string) (string, error) {
	incoming, outgoing := transitionIndexes(c.doc)
	for _, it := range c.trackItems(tr.TrackID) {
		normalItem := transitionAdjustedItem(it, incoming, outgoing)
		overlayItem := normalItem
		var itemRef string
		var err error
		if transition, exists := incoming[it.ItemID]; exists {
			offset := transition.Frames
			if offset < it.DurationInFrames {
				itemRef, err = c.emitVisualSlice(
					"item:"+it.ItemID+":after-transition",
					it,
					offset,
					it.DurationInFrames-offset,
					false,
				)
				overlayItem.From += offset
				overlayItem.DurationInFrames -= offset
			}
		} else {
			itemRef, err = c.emitItemPrep(tr, normalItem, true)
		}
		if err != nil {
			return "", err
		}
		if itemRef != "" {
			baseRef = c.emitOverlayItem(baseRef, itemRef, overlayItem)
		}
		if transition, exists := outgoing[it.ItemID]; exists {
			to, ok := itemByID(c.doc, transition.ToItemID)
			if !ok {
				return "", fmt.Errorf("render: transition %q references missing to item", transition.TransitionID)
			}
			fromRef, prepErr := c.emitVisualSlice(
				"transition:"+transition.TransitionID+":from",
				it,
				it.DurationInFrames,
				transition.Frames,
				true,
			)
			if prepErr != nil {
				return "", prepErr
			}
			toRef, prepErr := c.emitVisualSlice(
				"transition:"+transition.TransitionID+":to",
				to,
				0,
				transition.Frames,
				false,
			)
			if prepErr != nil {
				return "", prepErr
			}
			fromRegion := itemPixelRegion(it, c.doc.Metadata.Width, c.doc.Metadata.Height)
			toRegion := itemPixelRegion(to, c.doc.Metadata.Width, c.doc.Metadata.Height)
			baseRef = c.emit("transition:"+transition.TransitionID+":overlay", map[string]any{
				"op": "transition_overlay",
				"params": map[string]any{
					"kind": transition.Kind, "start": c.sec(to.From),
					"duration": c.sec(transition.Frames),
					"from_x":   fromRegion.X, "from_y": fromRegion.Y,
					"from_w": fromRegion.Width, "from_h": fromRegion.Height,
					"from_opacity": fromRegion.Opacity,
					"to_x":         toRegion.X, "to_y": toRegion.Y,
					"to_w": toRegion.Width, "to_h": toRegion.Height,
					"to_opacity": toRegion.Opacity,
				},
				"video": baseRef, "video2": fromRef, "video3": toRef,
			})
		}
	}
	return baseRef, nil
}

func (c *compiler) emitOverlayItem(baseRef, itemRef string, item timeline.Item) string {
	region := itemPixelRegion(item, c.doc.Metadata.Width, c.doc.Metadata.Height)
	params := map[string]any{
		"x": region.X, "y": region.Y,
		"w": region.Width, "h": region.Height,
		"start": c.sec(item.From), "end": c.sec(item.End()),
	}
	if c.managedSources {
		params["opacity"], params["fit"], params["bg"] = region.Opacity, region.Fit, c.background
		params["fade_in"], params["fade_out"] = c.sec(item.FadeInFrames), c.sec(item.FadeOutFrames)
	}
	return c.emit("overlay:"+item.ItemID, map[string]any{
		"op":     "overlay_region",
		"params": params,
		"video":  baseRef,
		"video2": itemRef,
	})
}

func (c *compiler) emitVisualSlice(
	id string,
	item timeline.Item,
	timelineOffset int64,
	duration int64,
	handle bool,
) (string, error) {
	region := itemPixelRegion(item, c.doc.Metadata.Width, c.doc.Metadata.Height)
	prepWidth, prepHeight := encodedRegionDimensions(region)
	uri, err := c.mediaURI(item.MediaID)
	if err != nil {
		return "", err
	}
	if item.Kind == timeline.ItemImage {
		params := map[string]any{
			"duration": c.sec(duration),
			"w":        prepWidth, "h": prepHeight, "fps": c.fps,
			"with_audio": true, "fit": region.Fit, "bg": c.background,
		}
		addVisualTransformParams(params, item)
		return c.emit(id, map[string]any{"op": "freeze", "params": params, "image": uri}), nil
	}
	if item.Kind != timeline.ItemVideo {
		return "", fmt.Errorf("render: transition item %q is not visual", item.ItemID)
	}
	sourceOffset := float64(item.TrimStart) + float64(timelineOffset)*item.EffectiveSpeed()
	if handle {
		sourceOffset = float64(item.TrimStart) +
			float64(item.DurationInFrames)*item.EffectiveSpeed()
	}
	sourceDuration := float64(duration) * item.EffectiveSpeed() / float64(c.fps)
	clipParams := map[string]any{
		"start": sourceOffset / float64(c.fps), "duration": sourceDuration,
		"w": prepWidth, "h": prepHeight, "fit": region.Fit, "bg": c.background,
	}
	addVisualTransformParams(clipParams, item)
	steps := []map[string]any{{"op": "clip_av", "params": clipParams}}
	if item.EffectiveSpeed() != 1 {
		steps = append(steps, map[string]any{
			"op": "retime_av",
			"params": map[string]any{
				"target_duration": c.sec(duration),
				"w":               prepWidth, "h": prepHeight, "fit": region.Fit, "bg": c.background,
			},
		})
	}
	return c.emit(id, map[string]any{"pipeline": steps, "video": uri}), nil
}

// ---- 音轨混音 ----

// compileAudioMix 把全部 audio 轨片段一次 mix_audio 进画面。
func (c *compiler) compileAudioMix(tracks []timeline.Track, baseRef string) (string, error) {
	type entry struct {
		ref      string
		offsetMs int64
		gain     float64
		envelope []map[string]any
	}
	var entries []entry
	totalFrames := timeline.DocDurationInFrames(c.doc)

	for _, tr := range tracks {
		if tr.Muted {
			continue
		}
		trackGain := effectiveVolume(tr.Volume)
		trackEnvelope := duckEnvelope(c.doc, tr, totalFrames)
		for _, it := range c.trackItems(tr.TrackID) {
			if it.Muted {
				continue
			}
			gain := trackGain * effectiveVolume(it.Volume)
			ref, err := c.emitAudioPrep("audio:"+it.ItemID, it)
			if err != nil {
				return "", err
			}
			envelope := clipGainEnvelope(trackEnvelope, it.From, it.End(), c.fps)
			if len(envelope) > maxEnvelopePoints {
				return "", fmt.Errorf(
					"render: duck envelope for item %q exceeds %d keyframes",
					it.ItemID, maxEnvelopePoints,
				)
			}
			entries = append(entries, entry{
				ref: ref, offsetMs: c.msOf(it.From), gain: gain,
				envelope: envelope,
			})
		}
	}
	// Local composition preserves audio from the direct base track in-place.
	// Video clips on overlay tracks (or every video clip when the first track
	// is transformed onto a generated canvas) are extracted and mixed here.
	if c.managedSources {
		incoming, outgoing := transitionIndexes(c.doc)
		for _, tr := range c.doc.Tracks {
			if tr.Kind != timeline.TrackVideo || tr.TrackID == c.baseAudioTrackID || tr.Muted {
				continue
			}
			trackGain := effectiveVolume(tr.Volume)
			for _, it := range c.trackItems(tr.TrackID) {
				if it.Kind != timeline.ItemVideo {
					continue
				}
				audioItem := transitionAdjustedItem(it, incoming, outgoing)
				var ref string
				var prepErr error
				if it.Muted {
					ref = ""
				} else if transition, exists := incoming[it.ItemID]; exists {
					offset := transition.Frames
					if offset >= it.DurationInFrames {
						ref = ""
					} else {
						audioItem.From += offset
						audioItem.DurationInFrames -= offset
						ref, prepErr = c.emitAudioSliceWithFades(
							"embedded-audio:"+it.ItemID,
							it, offset, it.DurationInFrames-offset, false,
							audioItem.FadeInFrames, audioItem.FadeOutFrames)
					}
				} else {
					ref, prepErr = c.emitAudioPrep("embedded-audio:"+it.ItemID, audioItem)
				}
				if prepErr != nil {
					return "", prepErr
				}
				if ref != "" {
					entries = append(entries, entry{
						ref: ref, offsetMs: c.msOf(audioItem.From),
						gain: trackGain * effectiveVolume(it.Volume),
					})
				}
			}
			for _, transition := range c.doc.Transitions {
				from, fromOK := itemByID(c.doc, transition.FromItemID)
				to, toOK := itemByID(c.doc, transition.ToItemID)
				if !fromOK || !toOK || from.TrackID != tr.TrackID {
					continue
				}
				fromRef, transitionErr := c.emitAudioSlice(
					"transition:"+transition.TransitionID+":from-audio",
					from, from.DurationInFrames, transition.Frames, true)
				if transitionErr != nil {
					return "", transitionErr
				}
				toRef, transitionErr := c.emitAudioSlice(
					"transition:"+transition.TransitionID+":to-audio",
					to, 0, transition.Frames, false)
				if transitionErr != nil {
					return "", transitionErr
				}
				transitionRef := c.emit("transition:"+transition.TransitionID+":audio", map[string]any{
					"op": "transition_audio",
					"params": map[string]any{
						"kind": transition.Kind, "duration": c.sec(transition.Frames),
						"from_gain": effectiveTransitionGain(trackGain, from),
						"to_gain":   effectiveTransitionGain(trackGain, to),
					},
					"audio": fromRef, "audio2": toRef,
				})
				entries = append(entries, entry{
					ref: transitionRef, offsetMs: c.msOf(to.From), gain: 1,
				})
			}
		}
	}
	if len(entries) == 0 {
		return baseRef, nil
	}

	audios := make([]string, len(entries))
	offsets := make([]int64, len(entries))
	gains := make([]float64, len(entries))
	envelopes := make([][]map[string]any, len(entries))
	for i, e := range entries {
		audios[i], offsets[i], gains[i] = e.ref, e.offsetMs, e.gain
		envelopes[i] = e.envelope
	}
	params := map[string]any{"offsets_ms": offsets, "gains": gains}
	for _, envelope := range envelopes {
		if len(envelope) > 0 {
			params["envelopes"] = envelopes
			break
		}
	}
	return c.emit("audiomix", map[string]any{
		"op":     "mix_audio",
		"params": params,
		"video":  baseRef,
		"audios": audios,
	}), nil
}

// emitAudioPrep 把音频片段裁到精确时长。
// trimStart>0 走 cut（catalog 标注输入槽位是 video，对纯音频文件的行为
// 需在 kess 环境实测确认——见 README 渲染路径章节）。
func (c *compiler) emitAudioPrep(id string, it timeline.Item) (string, error) {
	uri, err := c.mediaURI(it.MediaID)
	if err != nil {
		return "", err
	}
	if c.managedSources {
		return c.emit(id, map[string]any{
			"op": "prepare_audio",
			"params": map[string]any{
				"start":           c.sec(it.TrimStart),
				"source_duration": c.sourceSeconds(it),
				"target_duration": c.sec(it.DurationInFrames),
				"fade_in":         c.sec(it.FadeInFrames),
				"fade_out":        c.sec(it.FadeOutFrames),
			},
			"audio": uri,
		}), nil
	}
	if it.TrimStart > 0 {
		c.warnKESSf("音频片段 %s 带 trimStart，cut op 对纯音频的支持待 KESS 实测", it.ItemID)
		return c.emit(id, map[string]any{
			"pipeline": []map[string]any{
				{"op": "cut", "params": map[string]any{
					"start": c.sec(it.TrimStart),
					"end":   c.sec(it.TrimStart + it.DurationInFrames),
				}},
				{"op": "fit_audio_duration", "params": map[string]any{
					"target_duration": c.sec(it.DurationInFrames),
				}},
			},
			"video": uri,
		}), nil
	}
	return c.emit(id, map[string]any{
		"op":     "fit_audio_duration",
		"params": map[string]any{"target_duration": c.sec(it.DurationInFrames)},
		"audio":  uri,
	}), nil
}

func (c *compiler) emitAudioSlice(
	id string,
	item timeline.Item,
	timelineOffset int64,
	duration int64,
	handle bool,
) (string, error) {
	return c.emitAudioSliceWithFades(id, item, timelineOffset, duration, handle, 0, 0)
}

func (c *compiler) emitAudioSliceWithFades(
	id string,
	item timeline.Item,
	timelineOffset int64,
	duration int64,
	handle bool,
	fadeInFrames int64,
	fadeOutFrames int64,
) (string, error) {
	uri, err := c.mediaURI(item.MediaID)
	if err != nil {
		return "", err
	}
	sourceOffset := float64(item.TrimStart) + float64(timelineOffset)*item.EffectiveSpeed()
	if handle {
		sourceOffset = float64(item.TrimStart) +
			float64(item.DurationInFrames)*item.EffectiveSpeed()
	}
	return c.emit(id, map[string]any{
		"op": "prepare_audio",
		"params": map[string]any{
			"start":           sourceOffset / float64(c.fps),
			"source_duration": float64(duration) * item.EffectiveSpeed() / float64(c.fps),
			"target_duration": c.sec(duration),
			"fade_in":         c.sec(fadeInFrames),
			"fade_out":        c.sec(fadeOutFrames),
		},
		"audio": uri,
	}), nil
}

// ---- 字幕 ----

// compileCaptions 把 caption 轨编译成 SRT 并烧录。
func (c *compiler) compileCaptions(tracks []timeline.Track, baseRef string) (string, error) {
	var items []timeline.Item
	for _, tr := range tracks {
		if tr.Muted {
			c.warnf("字幕轨 %s 处于隐藏（muted）状态，跳过烧录", tr.TrackID)
			continue
		}
		items = append(items, c.trackItems(tr.TrackID)...)
	}
	if len(items) == 0 {
		return baseRef, nil
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].From != items[b].From {
			return items[a].From < items[b].From
		}
		return items[a].ItemID < items[b].ItemID
	})
	if len(items) > maxSubtitleEntries {
		return "", fmt.Errorf(
			"render: caption count exceeds %d entries", maxSubtitleEntries)
	}
	for _, item := range items {
		if len(item.Text) == 0 || len(item.Text) > maxStyledSubtitleTextSize {
			return "", fmt.Errorf(
				"render: caption %q text must be within 1 byte..16 KiB",
				item.ItemID,
			)
		}
	}
	if c.managedSources && hasStyledCaptions(items) {
		payload, payloadErr := buildStyledCaptionPayload(
			items, c.fps, c.doc.Metadata.Height)
		if payloadErr != nil {
			return "", payloadErr
		}
		return c.emit("captions", map[string]any{
			"op":       "burn_subtitles",
			"params":   map[string]any{},
			"video":    baseRef,
			"subtitle": "styled-caption-v1:" + base64.StdEncoding.EncodeToString(payload),
		}), nil
	}
	srt := BuildSRT(items, c.fps)
	if len(srt) > int((maxSubtitleBytes+2)/3*4) {
		return "", fmt.Errorf("render: decoded subtitle content exceeds 1 MiB")
	}
	for _, item := range items {
		for _, r := range item.Text {
			if r > 127 {
				c.warnKESSf("字幕包含非 ASCII 字符；目标 ffmpeg-worker 必须安装或显式提供 CJK 字体")
				goto captionsReady
			}
		}
	}
captionsReady:
	return c.emit("captions", map[string]any{
		"op":       "burn_subtitles",
		"params":   map[string]any{},
		"video":    baseRef,
		"subtitle": "base64:" + srt,
	}), nil
}

// effectiveVolume 归一化音量（0 表示未设置按 1 处理，与 timeline 语义一致）。
func effectiveVolume(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func effectiveTransitionGain(trackGain float64, item timeline.Item) float64 {
	if item.Muted {
		return 0
	}
	return trackGain * effectiveVolume(item.Volume)
}
