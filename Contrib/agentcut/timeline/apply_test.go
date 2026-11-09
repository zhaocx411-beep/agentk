package timeline

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }
func str(v string) *string   { return &v }
func iptr(v int) *int        { return &v }
func bptr(v bool) *bool      { return &v }

// baseDoc：1080p30，一条 300 帧视频素材 + 一条 600 帧音频素材，
// 轨道 v1(视频)、a1(音频)、c1(字幕)、vlocked(锁定视频轨)。
func baseDoc() Doc {
	d := NewDoc("test", 1920, 1080, 30)
	d.Media = []Media{
		{MediaID: "m_video", URI: "blob://v", Kind: MediaVideo, DurationInFrames: 300},
		{MediaID: "m_audio", URI: "blob://a", Kind: MediaAudio, DurationInFrames: 600},
		{MediaID: "m_image", URI: "blob://i", Kind: MediaImage},
	}
	d.Tracks = []Track{
		{TrackID: "v1", Kind: TrackVideo},
		{TrackID: "a1", Kind: TrackAudio},
		{TrackID: "c1", Kind: TrackCaption},
		{TrackID: "vlocked", Kind: TrackVideo, Locked: true},
	}
	return d
}

// docWithClip：v1 轨上放一段 [100, 220)、trimStart=30 的视频片段。
func docWithClip() Doc {
	d := baseDoc()
	d.Items = []Item{{
		ItemID: "it1", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 100, DurationInFrames: 120, TrimStart: 30,
	}}
	return d
}

func mustApply(t *testing.T, d Doc, ops ...Operation) (Doc, ChangeSummary) {
	t.Helper()
	next, sum, err := ApplyOps(d, ops)
	require.NoError(t, err)
	return next, sum
}

func mustFail(t *testing.T, d Doc, code ErrorCode, ops ...Operation) *OpError {
	t.Helper()
	_, _, err := ApplyOps(d, ops)
	require.Error(t, err)
	var oe *OpError
	require.ErrorAs(t, err, &oe)
	assert.Equal(t, code, oe.Code, "unexpected error: %v", oe)
	return oe
}

func TestApplyOpsBatchGuards(t *testing.T) {
	d := baseDoc()
	mustFail(t, d, ErrBadRequest) // 空批
	ops := make([]Operation, MaxOpsPerBatch+1)
	for i := range ops {
		ops[i] = Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{Name: str("x")}}
	}
	mustFail(t, d, ErrLimitExceeded, ops...)

	bad := d
	bad.SchemaVersion = 99
	mustFail(t, bad, ErrUnsupported, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{Name: str("x")}})
	mustFail(t, d, ErrUnsupported, Operation{Op: "noSuchOp"})
}

func TestInputDocNotMutated(t *testing.T) {
	d := docWithClip()
	before, _ := json.Marshal(d)

	// 成功路径
	_, _ = mustApply(t, d, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(160)})
	after, _ := json.Marshal(d)
	assert.JSONEq(t, string(before), string(after), "success path mutated input")

	// 失败路径
	_, _, err := ApplyOps(d, []Operation{{Op: OpRemoveItems, ItemIDs: []string{"nope"}}})
	require.Error(t, err)
	after2, _ := json.Marshal(d)
	assert.JSONEq(t, string(before), string(after2), "failure path mutated input")
}

func TestDeterminism(t *testing.T) {
	d := docWithClip()
	ops := []Operation{
		{Op: OpSplit, ItemID: "it1", Frame: i64(150)},
		{Op: OpMoveItem, ItemID: "it1~150", From: i64(400)},
	}
	a, _, err1 := ApplyOps(d, ops)
	b, _, err2 := ApplyOps(d, ops)
	require.NoError(t, err1)
	require.NoError(t, err2)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	assert.Equal(t, string(ja), string(jb))
}

// ---- media ----

func TestAddRemoveMedia(t *testing.T) {
	d := baseDoc()
	next, sum := mustApply(t, d, Operation{Op: OpAddMedia, Media: &Media{
		MediaID: "m_new", URI: "blob://n", Kind: MediaVideo, DurationInFrames: 90,
	}})
	assert.Len(t, next.Media, 4)
	assert.Equal(t, []string{"m_new"}, sum.AddedMediaIDs)

	mustFail(t, d, ErrConflictingID, Operation{Op: OpAddMedia, Media: &Media{MediaID: "m_video", URI: "x", Kind: MediaVideo, DurationInFrames: 1}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddMedia, Media: &Media{MediaID: "m2", URI: "x", Kind: MediaVideo}})                      // 视频无时长
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddMedia, Media: &Media{MediaID: "m3", URI: "x", Kind: MediaImage, DurationInFrames: 5}}) // 图片带时长
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddMedia, Media: &Media{MediaID: "m4", URI: "x", Kind: "gif", DurationInFrames: 5}})

	// 被引用的 media 不能删
	dc := docWithClip()
	mustFail(t, dc, ErrStillReferenced, Operation{Op: OpRemoveMedia, MediaID: "m_video"})
	next2, sum2 := mustApply(t, dc, Operation{Op: OpRemoveMedia, MediaID: "m_audio"})
	assert.Len(t, next2.Media, 2)
	assert.Equal(t, []string{"m_audio"}, sum2.RemovedMediaIDs)
	mustFail(t, dc, ErrNotFound, Operation{Op: OpRemoveMedia, MediaID: "nope"})
}

func TestUpdateMedia(t *testing.T) {
	d := baseDoc()
	next, sum := mustApply(t, d, Operation{
		Op: OpUpdateMedia, MediaID: "m_video", MediaPatch: &MediaPatch{DisplayName: str("重命名后的素材")},
	})
	assert.Equal(t, "重命名后的素材", next.Media[0].DisplayName)
	assert.Equal(t, []string{"m_video"}, sum.UpdatedMediaIDs)

	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateMedia, MediaID: "m_video"})
	mustFail(t, d, ErrNotFound, Operation{Op: OpUpdateMedia, MediaID: "nope", MediaPatch: &MediaPatch{DisplayName: str("x")}})
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateMedia, MediaID: "m_video", MediaPatch: &MediaPatch{DisplayName: str(strings.Repeat("a", 257))},
	})

	// 空白字符串是显式"清空覆盖名"，撤销首次重命名要能精确恢复到这个状态。
	next2, _ := mustApply(t, next, Operation{
		Op: OpUpdateMedia, MediaID: "m_video", MediaPatch: &MediaPatch{DisplayName: str("   ")},
	})
	assert.Empty(t, next2.Media[0].DisplayName)
}

func TestUpdateMediaProcessingFields(t *testing.T) {
	d := baseDoc()
	duration := int64(180)
	sourceKey, sourceURI := "agentcut/p/source/m_video/video.mp4", "https://blob.example/video.mp4"
	thumbnailKey, thumbnailURI := "agentcut/p/thumbnails/m_video.jpg", "https://blob.example/thumb.jpg"
	peaks := []float32{0.1, 0.5, 1}
	understanding := "一段产品演示视频"
	probe := &MediaProbe{DurationMs: 6000, Width: 1280, Height: 720, HasAudio: true}
	transcript := &Transcript{Language: "zh", Segments: []TranscriptSegment{{
		StartMs: 100, EndMs: 900, Text: "你好",
	}}}
	processing := &MediaProcessing{
		Status: "ready", ReadyForTimeline: true,
		Probe: ProcessingSucceeded, Thumbnail: ProcessingSucceeded,
		Waveform: ProcessingSucceeded, Transcript: ProcessingSucceeded,
		Understanding: ProcessingSucceeded,
	}
	next, sum := mustApply(t, d, Operation{
		Op: OpUpdateMedia, MediaID: "m_video",
		MediaPatch: &MediaPatch{
			URI: &sourceURI, BlobKey: &sourceKey,
			DurationInFrames: &duration, Probe: probe,
			ThumbnailKey: &thumbnailKey, ThumbnailURI: &thumbnailURI,
			WaveformPeaks: &peaks, Transcript: transcript,
			Processing: processing, UnderstandingText: &understanding,
		},
	})
	got := next.Media[0]
	assert.Equal(t, sourceURI, got.URI)
	assert.Equal(t, sourceKey, got.BlobKey)
	assert.Equal(t, duration, got.DurationInFrames)
	assert.Equal(t, probe, got.Probe)
	assert.Equal(t, peaks, got.WaveformPeaks)
	require.NotNil(t, got.Transcript)
	assert.Equal(t, uint64(1), got.Transcript.Revision)
	assert.Equal(t, transcript.Language, got.Transcript.Language)
	assert.Equal(t, transcript.Segments[0].Text, got.Transcript.Segments[0].Text)
	assert.NotEmpty(t, got.Transcript.Segments[0].SegmentID)
	assert.Equal(t, processing, got.Processing)
	assert.Equal(t, understanding, got.UnderstandingText)
	assert.Equal(t, []string{"m_video"}, sum.UpdatedMediaIDs)

	peaks[0] = 0.9
	transcript.Segments[0].Text = "mutated"
	processing.Status = "failed"
	assert.Equal(t, float32(0.1), got.WaveformPeaks[0], "patch slices must be cloned")
	assert.Equal(t, "你好", got.Transcript.Segments[0].Text)
	assert.Equal(t, "ready", got.Processing.Status)

	invalidPeaks := []float32{-0.1}
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateMedia, MediaID: "m_video", MediaPatch: &MediaPatch{WaveformPeaks: &invalidPeaks},
	})
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateMedia, MediaID: "m_video", MediaPatch: &MediaPatch{Processing: &MediaProcessing{
			Status: "ready", ReadyForTimeline: true,
			Probe: "unknown", Thumbnail: ProcessingSkipped, Waveform: ProcessingSkipped,
			Transcript: ProcessingSkipped, Understanding: ProcessingSkipped,
		}},
	})
}

func TestSetTranscript(t *testing.T) {
	d := baseDoc()
	tr := &Transcript{Language: "zh", Segments: []TranscriptSegment{
		{StartMs: 0, EndMs: 1200, Text: "大家好", Words: []TranscriptWord{{StartMs: 0, EndMs: 400, Text: "大家"}}},
	}}
	next, sum := mustApply(t, d, Operation{Op: OpSetTranscript, MediaID: "m_video", Transcript: tr})
	require.NotNil(t, next.Media[0].Transcript)
	assert.Equal(t, "大家好", next.Media[0].Transcript.Segments[0].Text)
	assert.Equal(t, []string{"m_video"}, sum.UpdatedMediaIDs)

	mustFail(t, d, ErrNotFound, Operation{Op: OpSetTranscript, MediaID: "nope", Transcript: tr})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSetTranscript, MediaID: "m_video"})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSetTranscript, MediaID: "m_video", Transcript: &Transcript{
		Segments: []TranscriptSegment{{StartMs: 100, EndMs: 50}},
	}})
}

// ---- addItem / removeItems ----

func TestAddItem(t *testing.T) {
	d := baseDoc()
	next, sum := mustApply(t, d, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it_a", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 0, DurationInFrames: 300,
	}})
	assert.Len(t, next.Items, 1)
	assert.Equal(t, []string{"it_a"}, sum.AddedItemIDs)

	// 各种失败
	mk := func(mut func(*Item)) Operation {
		it := Item{ItemID: "it_x", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 0, DurationInFrames: 100}
		mut(&it)
		return Operation{Op: OpAddItem, Item: &it}
	}
	mustFail(t, d, ErrNotFound, mk(func(it *Item) { it.TrackID = "nope" }))
	mustFail(t, d, ErrLockedTrack, mk(func(it *Item) { it.TrackID = "vlocked" }))
	mustFail(t, d, ErrKindMismatch, mk(func(it *Item) { it.TrackID = "a1" }))      // 视频进音轨
	mustFail(t, d, ErrKindMismatch, mk(func(it *Item) { it.MediaID = "m_audio" })) // 视频 item 引音频素材
	mustFail(t, d, ErrNotFound, mk(func(it *Item) { it.MediaID = "nope" }))
	mustFail(t, d, ErrOutOfRange, mk(func(it *Item) { it.DurationInFrames = 301 })) // 超源长
	mustFail(t, d, ErrOutOfRange, mk(func(it *Item) { it.TrimStart = 250; it.DurationInFrames = 100 }))
	mustFail(t, d, ErrOutOfRange, mk(func(it *Item) { it.From = -1 }))
	mustFail(t, d, ErrBadRequest, mk(func(it *Item) { it.Speed = 20 }))
	mustFail(t, d, ErrBadRequest, mk(func(it *Item) { it.Volume = 3 }))

	// 变速：2x 速播 100 帧消耗 200 源帧
	mustApply(t, d, mk(func(it *Item) { it.Speed = 2; it.DurationInFrames = 150 }))
	mustFail(t, d, ErrOutOfRange, mk(func(it *Item) { it.Speed = 2; it.DurationInFrames = 151 }))

	// 图片 item 时长不受源长限制
	mustApply(t, d, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it_img", TrackID: "v1", Kind: ItemImage, MediaID: "m_image", From: 0, DurationInFrames: 9999,
	}})

	// 文本 item：必须有文字、不能引素材
	mustApply(t, d, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it_t", TrackID: "c1", Kind: ItemText, From: 0, DurationInFrames: 60, Text: "你好",
	}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it_t2", TrackID: "c1", Kind: ItemText, From: 0, DurationInFrames: 60, Text: "  ",
	}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it_t3", TrackID: "c1", Kind: ItemText, MediaID: "m_video", From: 0, DurationInFrames: 60, Text: "x",
	}})

	// 重复 ID
	dc := docWithClip()
	mustFail(t, dc, ErrConflictingID, Operation{Op: OpAddItem, Item: &Item{
		ItemID: "it1", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 500, DurationInFrames: 10,
	}})
}

func TestRemoveItems(t *testing.T) {
	d := docWithClip()
	next, sum := mustApply(t, d, Operation{Op: OpRemoveItems, ItemIDs: []string{"it1"}})
	assert.Empty(t, next.Items)
	assert.Equal(t, []string{"it1"}, sum.RemovedItemIDs)

	mustFail(t, d, ErrNotFound, Operation{Op: OpRemoveItems, ItemIDs: []string{"it1", "nope"}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpRemoveItems})

	// 锁定轨上的 item 不能删
	locked := d
	locked.Items = append([]Item(nil), d.Items...)
	locked.Items[0].TrackID = "vlocked"
	mustFail(t, locked, ErrLockedTrack, Operation{Op: OpRemoveItems, ItemIDs: []string{"it1"}})
}

// ---- split / trim ----

func TestSplitConservesSource(t *testing.T) {
	d := docWithClip() // it1: from=100 dur=120 trim=30，源区间 [30, 150)
	next, sum := mustApply(t, d, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(160)})
	require.Len(t, next.Items, 2)

	left, right := next.Items[0], next.Items[1]
	assert.Equal(t, "it1", left.ItemID)
	assert.Equal(t, int64(100), left.From)
	assert.Equal(t, int64(60), left.DurationInFrames) // 100..160
	assert.Equal(t, int64(30), left.TrimStart)

	assert.Equal(t, "it1~160", right.ItemID)
	assert.Equal(t, int64(160), right.From)
	assert.Equal(t, int64(60), right.DurationInFrames) // 160..220
	assert.Equal(t, int64(90), right.TrimStart)        // 30 + 60

	// 源区间守恒：left 消耗 [30,90)，right 消耗 [90,150)
	assert.Equal(t, left.TrimStart+left.SourceFramesConsumed(), right.TrimStart)
	total := left.SourceFramesConsumed() + right.SourceFramesConsumed()
	assert.Equal(t, int64(120), total)
	assert.Contains(t, sum.AddedItemIDs, "it1~160")

	// 边界：分割点必须严格在内部
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(100)})
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(220)})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSplit, ItemID: "it1"})

	// 自定义右段 ID
	next2, _ := mustApply(t, d, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(160), NewItemID: "custom"})
	assert.Equal(t, "custom", next2.Items[1].ItemID)
}

func TestSplitWithSpeed(t *testing.T) {
	d := baseDoc()
	d.Items = []Item{{
		ItemID: "fast", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 0, DurationInFrames: 100, Speed: 2, // 消耗源 [0, 200)
	}}
	next, _ := mustApply(t, d, Operation{Op: OpSplit, ItemID: "fast", Frame: i64(40)})
	right := next.Items[1]
	assert.Equal(t, int64(80), right.TrimStart) // 40 帧 × 2x = 80 源帧
	assert.Equal(t, int64(60), right.DurationInFrames)
}

func TestTrim(t *testing.T) {
	d := docWithClip() // it1: [100,220) trim=30，源 [30,150)/300

	// 左沿右移收窄
	next, _ := mustApply(t, d, Operation{Op: OpTrimStart, ItemID: "it1", Frame: i64(130)})
	it := next.Items[0]
	assert.Equal(t, int64(130), it.From)
	assert.Equal(t, int64(90), it.DurationInFrames)
	assert.Equal(t, int64(60), it.TrimStart)

	// 左沿左移扩展（源头还有 30 帧余量）
	next, _ = mustApply(t, d, Operation{Op: OpTrimStart, ItemID: "it1", Frame: i64(70)})
	it = next.Items[0]
	assert.Equal(t, int64(70), it.From)
	assert.Equal(t, int64(150), it.DurationInFrames)
	assert.Equal(t, int64(0), it.TrimStart)
	// 再往左超出源头 → 失败
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpTrimStart, ItemID: "it1", Frame: i64(69)})

	// 右沿扩展（源尾还有 150 帧余量）
	next, _ = mustApply(t, d, Operation{Op: OpTrimEnd, ItemID: "it1", Frame: i64(370)})
	assert.Equal(t, int64(270), next.Items[0].DurationInFrames)
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpTrimEnd, ItemID: "it1", Frame: i64(371)})

	// 右沿收窄
	next, _ = mustApply(t, d, Operation{Op: OpTrimEnd, ItemID: "it1", Frame: i64(150)})
	assert.Equal(t, int64(50), next.Items[0].DurationInFrames)
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpTrimEnd, ItemID: "it1", Frame: i64(100)})
}

// ---- move / update ----

func TestMoveItem(t *testing.T) {
	d := docWithClip()
	next, _ := mustApply(t, d, Operation{Op: OpMoveItem, ItemID: "it1", From: i64(500)})
	assert.Equal(t, int64(500), next.Items[0].From)

	// 跨轨移动类别必须兼容
	mustFail(t, d, ErrKindMismatch, Operation{Op: OpMoveItem, ItemID: "it1", TrackID: "a1", From: i64(0)})
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpMoveItem, ItemID: "it1", TrackID: "vlocked", From: i64(0)})
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpMoveItem, ItemID: "it1", From: i64(-1)})

	// 移到另一条视频轨 OK
	d2 := docWithClip()
	d2.Tracks = append(d2.Tracks, Track{TrackID: "v2", Kind: TrackVideo})
	next2, _ := mustApply(t, d2, Operation{Op: OpMoveItem, ItemID: "it1", TrackID: "v2", From: i64(0)})
	assert.Equal(t, "v2", next2.Items[0].TrackID)
}

func TestUpdateItem(t *testing.T) {
	d := docWithClip()
	next, _ := mustApply(t, d, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{
		Speed: f64(1.25), Volume: f64(0.5),
	}})
	assert.Equal(t, 1.25, next.Items[0].Speed)
	assert.Equal(t, 0.5, next.Items[0].Volume)

	// 变速导致源消耗越界：trim=30 + 120*2.5=300 > 300？ 30+300=330 > 300 → fail
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Speed: f64(2.5)}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Speed: f64(0.01)}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Text: str("x")}}) // 非文本 item
	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "it1"})

	// 文本 item 改字
	dt := baseDoc()
	dt.Items = []Item{{ItemID: "cap", TrackID: "c1", Kind: ItemText, From: 0, DurationInFrames: 60, Text: "旧"}}
	next2, _ := mustApply(t, dt, Operation{Op: OpUpdateItem, ItemID: "cap", Patch: &ItemPatch{Text: str("新"), StylePresetID: str("style-2")}})
	assert.Equal(t, "新", next2.Items[0].Text)
	assert.Equal(t, "style-2", next2.Items[0].StylePresetID)
	mustFail(t, dt, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "cap", Patch: &ItemPatch{Volume: f64(1)}}) // 文本无音量
}

func TestItemMuteFadeAndTransformValidation(t *testing.T) {
	d := docWithClip()
	transform := ItemTransform{X: 0.5, Y: 0.1, Width: 0.4, Height: 0.5, Opacity: 0.75, Fit: ItemFitCover}
	next, _ := mustApply(t, d, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{
		Muted: bptr(true), FadeInFrames: i64(10), FadeOutFrames: i64(20), Transform: &transform,
	}})
	item := next.Items[0]
	assert.True(t, item.Muted)
	assert.Equal(t, int64(10), item.FadeInFrames)
	assert.Equal(t, int64(20), item.FadeOutFrames)
	require.NotNil(t, item.Transform)
	assert.Equal(t, ItemFitCover, item.Transform.Fit)

	// Clone must isolate the pointer-valued transform.
	cloned := next.Clone()
	cloned.Items[0].Transform.X = 0
	assert.Equal(t, 0.5, next.Items[0].Transform.X)

	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{FadeInFrames: i64(-1)}})
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{
		FadeInFrames: i64(80), FadeOutFrames: i64(50),
	}})
	invalidTransform := ItemTransform{X: 0.8, Y: 0, Width: 0.3, Height: 1, Fit: ItemFitContain}
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Transform: &invalidTransform}})
	invalidFit := ItemTransform{X: 0, Y: 0, Width: 1, Height: 1, Fit: "stretch"}
	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Transform: &invalidFit}})

	imageDoc := baseDoc()
	imageDoc.Items = []Item{{
		ItemID: "img", TrackID: "v1", Kind: ItemImage, MediaID: "m_image",
		From: 0, DurationInFrames: 30, FadeInFrames: 1,
	}}
	assert.NotNil(t, ValidateDoc(imageDoc), "image fades must be rejected")

	audioDoc := baseDoc()
	audioDoc.Items = []Item{{
		ItemID: "aud", TrackID: "a1", Kind: ItemAudio, MediaID: "m_audio",
		From: 0, DurationInFrames: 30, Transform: &ItemTransform{Width: 1, Height: 1},
	}}
	assert.NotNil(t, ValidateDoc(audioDoc), "audio transforms must be rejected")
}

func TestFadeSemanticsAcrossSplitTrimAndRippleDelete(t *testing.T) {
	d := docWithClip()
	d.Items[0].FadeInFrames = 20
	d.Items[0].FadeOutFrames = 30

	split, _ := mustApply(t, d, Operation{Op: OpSplit, ItemID: "it1", Frame: i64(160)})
	require.Len(t, split.Items, 2)
	assert.Equal(t, int64(20), split.Items[0].FadeInFrames)
	assert.Zero(t, split.Items[0].FadeOutFrames)
	assert.Zero(t, split.Items[1].FadeInFrames)
	assert.Equal(t, int64(30), split.Items[1].FadeOutFrames)

	trimmedStart, _ := mustApply(t, d, Operation{Op: OpTrimStart, ItemID: "it1", Frame: i64(115)})
	assert.Equal(t, int64(5), trimmedStart.Items[0].FadeInFrames)
	assert.Equal(t, int64(30), trimmedStart.Items[0].FadeOutFrames)
	trimmedEnd, _ := mustApply(t, d, Operation{Op: OpTrimEnd, ItemID: "it1", Frame: i64(200)})
	assert.Equal(t, int64(10), trimmedEnd.Items[0].FadeOutFrames)

	spanning := baseDoc()
	spanning.Items = []Item{{
		ItemID: "span", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 0, DurationInFrames: 300, FadeInFrames: 20, FadeOutFrames: 30,
	}}
	rippled, _ := mustApply(t, spanning, Operation{
		Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(150),
	})
	require.Len(t, rippled.Items, 2)
	assert.Equal(t, int64(20), rippled.Items[0].FadeInFrames)
	assert.Zero(t, rippled.Items[0].FadeOutFrames)
	assert.Zero(t, rippled.Items[1].FadeInFrames)
	assert.Equal(t, int64(30), rippled.Items[1].FadeOutFrames)
	require.Nil(t, ValidateDoc(rippled))
}

func TestFractionalSpeedEditsRequireSourceFrameAlignedBoundaries(t *testing.T) {
	d := baseDoc()
	d.Items = []Item{{
		ItemID: "slow", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 0, DurationInFrames: 100, Speed: 0.5,
	}}
	mustFail(t, d, ErrOutOfRange, Operation{
		Op: OpSplit, ItemID: "slow", Frame: i64(1),
	})
	aligned, _ := mustApply(t, d, Operation{
		Op: OpSplit, ItemID: "slow", Frame: i64(2),
	})
	assert.Equal(t, int64(1), aligned.Items[1].TrimStart)

	mustFail(t, d, ErrOutOfRange, Operation{
		Op: OpTrimStart, ItemID: "slow", Frame: i64(1),
	})
	mustFail(t, d, ErrOutOfRange, Operation{
		Op: OpRippleDelete, FromFrame: i64(0), ToFrame: i64(1),
	})

	atEOF := baseDoc()
	atEOF.Items = []Item{{
		ItemID: "past-end", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video",
		From: 0, DurationInFrames: 1, TrimStart: 300, Speed: 0.1,
	}}
	assert.NotNil(t, ValidateDoc(atEOF),
		"fractional source consumption beyond EOF must not round down to zero")
}

func TestLegacyItemJSONStillValid(t *testing.T) {
	const raw = `{
		"schemaVersion":1,
		"metadata":{"width":320,"height":180,"fps":30},
		"media":[{"mediaId":"m","uri":"u","kind":"video","durationInFrames":30}],
		"tracks":[{"trackId":"v","kind":"video"}],
		"items":[{"itemId":"i","trackId":"v","kind":"video","mediaId":"m","from":0,"durationInFrames":30}]
	}`
	var doc Doc
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	require.Nil(t, ValidateDoc(doc))
	assert.False(t, doc.Items[0].Muted)
	assert.Zero(t, doc.Items[0].FadeInFrames)
	assert.Nil(t, doc.Items[0].Transform)
}

// ---- rippleDelete ----

func TestRippleDelete(t *testing.T) {
	d := baseDoc()
	d.Items = []Item{
		{ItemID: "before", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 0, DurationInFrames: 50},
		{ItemID: "inside", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 100, DurationInFrames: 50},
		{ItemID: "after", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 200, DurationInFrames: 50},
		{ItemID: "aud", TrackID: "a1", Kind: ItemAudio, MediaID: "m_audio", From: 120, DurationInFrames: 300},
	}
	// 删除 [100, 150)：inside 整段消失，after 左移 50，aud 头部被裁
	next, sum := mustApply(t, d, Operation{Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(150)})

	byID := map[string]Item{}
	for _, it := range next.Items {
		byID[it.ItemID] = it
	}
	assert.Len(t, next.Items, 3)
	assert.Equal(t, int64(0), byID["before"].From)
	assert.NotContains(t, byID, "inside")
	assert.Equal(t, int64(150), byID["after"].From)
	// aud [120,420) 头部 30 帧落入区间：裁掉后起点回到 100，时长 270，trim+30
	assert.Equal(t, int64(100), byID["aud"].From)
	assert.Equal(t, int64(270), byID["aud"].DurationInFrames)
	assert.Equal(t, int64(30), byID["aud"].TrimStart)
	assert.Contains(t, sum.RemovedItemIDs, "inside")

	// 跨越整个区间的 item 裂成两段
	d2 := baseDoc()
	d2.Items = []Item{{ItemID: "span", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 0, DurationInFrames: 300}}
	next2, _ := mustApply(t, d2, Operation{Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(150)})
	require.Len(t, next2.Items, 2)
	l, r := next2.Items[0], next2.Items[1]
	assert.Equal(t, int64(100), l.DurationInFrames)
	assert.Equal(t, "span~150", r.ItemID)
	assert.Equal(t, int64(100), r.From)
	assert.Equal(t, int64(150), r.DurationInFrames)
	assert.Equal(t, int64(150), r.TrimStart)

	// 尾部落入区间：截断
	d3 := baseDoc()
	d3.Items = []Item{{ItemID: "tail", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 50, DurationInFrames: 100}}
	next3, _ := mustApply(t, d3, Operation{Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(200)})
	assert.Equal(t, int64(50), next3.Items[0].DurationInFrames)

	// 指定轨道时锁定轨报错；缺省时锁定轨不受影响
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpRippleDelete, FromFrame: i64(0), ToFrame: i64(10), TrackIDs: []string{"vlocked"}})
	dl := baseDoc()
	dl.Items = []Item{{ItemID: "lk", TrackID: "vlocked", Kind: ItemVideo, MediaID: "m_video", From: 200, DurationInFrames: 50}}
	next4, _ := mustApply(t, dl, Operation{Op: OpRippleDelete, FromFrame: i64(0), ToFrame: i64(100)})
	assert.Equal(t, int64(200), next4.Items[0].From, "locked track must not shift")

	mustFail(t, d, ErrOutOfRange, Operation{Op: OpRippleDelete, FromFrame: i64(100), ToFrame: i64(100)})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpRippleDelete, FromFrame: i64(100)})
}

// ---- tracks ----

func TestTracks(t *testing.T) {
	d := baseDoc()
	next, sum := mustApply(t, d,
		Operation{Op: OpAddTrack, Track: &Track{TrackID: "v2", Kind: TrackVideo}, Index: iptr(1)},
	)
	assert.Equal(t, "v2", next.Tracks[1].TrackID)
	assert.Equal(t, []string{"v2"}, sum.AddedTrackIDs)

	mustFail(t, d, ErrConflictingID, Operation{Op: OpAddTrack, Track: &Track{TrackID: "v1", Kind: TrackVideo}})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpAddTrack, Track: &Track{TrackID: "x", Kind: "weird"}})
	mustFail(t, d, ErrOutOfRange, Operation{Op: OpAddTrack, Track: &Track{TrackID: "x", Kind: TrackVideo}, Index: iptr(99)})

	// removeTrack 连带删 items
	dc := docWithClip()
	next2, sum2 := mustApply(t, dc, Operation{Op: OpRemoveTrack, TrackID: "v1"})
	assert.Empty(t, next2.Items)
	assert.Equal(t, []string{"it1"}, sum2.RemovedItemIDs)
	mustFail(t, dc, ErrLockedTrack, Operation{Op: OpRemoveTrack, TrackID: "vlocked"})

	// updateTrack 可以解锁锁定轨
	next3, _ := mustApply(t, d, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Locked: bptr(false)}})
	_, ok := next3.trackByID("vlocked")
	require.True(t, ok)
	for _, tr := range next3.Tracks {
		if tr.TrackID == "vlocked" {
			assert.False(t, tr.Locked)
		}
	}
	// 但锁定期间不能改其他字段（不能绕过轨道锁）
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Name: str("改名")}})
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Muted: bptr(true)}})
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Hidden: bptr(true)}})
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Volume: f64(0.5)}})
	// 解锁和改名混在同一个 patch 里也不行（先解锁 op、再改名 op）
	mustFail(t, d, ErrLockedTrack, Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Locked: bptr(false), Name: str("改名")}})
	// 同批内先解锁再改名 → 允许
	next5, _ := mustApply(t, d,
		Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Locked: bptr(false)}},
		Operation{Op: OpUpdateTrack, TrackID: "vlocked", TrackPatch: &TrackPatch{Name: str("已解锁")}},
	)
	for _, tr := range next5.Tracks {
		if tr.TrackID == "vlocked" {
			assert.False(t, tr.Locked)
			assert.Equal(t, "已解锁", tr.Name)
		}
	}
	// 音量与静音
	next4, _ := mustApply(t, d, Operation{Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Muted: bptr(true), Volume: f64(0.3), Name: str("BGM")}})
	for _, tr := range next4.Tracks {
		if tr.TrackID == "a1" {
			assert.True(t, tr.Muted)
			assert.Equal(t, 0.3, tr.Volume)
			assert.Equal(t, "BGM", tr.Name)
		}
	}
	mustFail(t, d, ErrBadRequest, Operation{Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Volume: f64(5)}})

	// 视频可见性与音频静音是两个独立字段；音频轨不能设置 hidden。
	hidden, _ := mustApply(t, d, Operation{
		Op: OpUpdateTrack, TrackID: "v1", TrackPatch: &TrackPatch{Hidden: bptr(true)},
	})
	for _, tr := range hidden.Tracks {
		if tr.TrackID == "v1" {
			assert.True(t, tr.Hidden)
			assert.False(t, tr.Muted)
		}
	}
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Hidden: bptr(true)},
	})
	assert.NotNil(t, ValidateDoc(Doc{
		SchemaVersion: SchemaVersion,
		Metadata:      d.Metadata,
		Media:         d.Media,
		Tracks:        []Track{{TrackID: "a1", Kind: TrackAudio, Hidden: true}},
		Items:         []Item{},
	}))
}

// ---- captions / metadata ----

func TestSetCaptions(t *testing.T) {
	d := baseDoc()
	d.Items = []Item{{ItemID: "old_cap", TrackID: "c1", Kind: ItemText, From: 0, DurationInFrames: 30, Text: "旧字幕"}}

	next, sum := mustApply(t, d, Operation{Op: OpSetCaptions, TrackID: "c1", Captions: []Caption{
		{From: 0, DurationInFrames: 60, Text: "第一句"},
		{From: 60, DurationInFrames: 45, Text: "第二句", StylePresetID: "style-1"},
	}})
	require.Len(t, next.Items, 2)
	assert.Equal(t, "c1_cap0", next.Items[0].ItemID)
	assert.Equal(t, "第一句", next.Items[0].Text)
	assert.Equal(t, "style-1", next.Items[1].StylePresetID)
	assert.Contains(t, sum.RemovedItemIDs, "old_cap")

	// 清空整轨
	next2, _ := mustApply(t, d, Operation{Op: OpSetCaptions, TrackID: "c1"})
	assert.Empty(t, next2.Items)

	mustFail(t, d, ErrKindMismatch, Operation{Op: OpSetCaptions, TrackID: "v1"})
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSetCaptions, TrackID: "c1", Captions: []Caption{{From: 0, DurationInFrames: 10, Text: " "}}})
	// 字幕重叠 → 批后校验失败
	mustFail(t, d, ErrOverlap, Operation{Op: OpSetCaptions, TrackID: "c1", Captions: []Caption{
		{From: 0, DurationInFrames: 60, Text: "a"},
		{From: 30, DurationInFrames: 60, Text: "b"},
	}})
}

func TestSetMetadata(t *testing.T) {
	// 空文档可以改 fps
	empty := NewDoc("x", 1920, 1080, 30)
	next, sum := mustApply(t, empty, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{FPS: iptr(60), Name: str("新名字")}})
	assert.Equal(t, 60, next.Metadata.FPS)
	assert.True(t, sum.MetadataChanged)

	mustFail(t, empty, ErrBadRequest, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{FPS: iptr(23)}})

	// 有素材/片段后禁改 fps，但可以改画布
	d := docWithClip()
	mustFail(t, d, ErrFPSLocked, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{FPS: iptr(60)}})
	next2, _ := mustApply(t, d, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{Width: iptr(1080), Height: iptr(1920)}})
	assert.Equal(t, 1080, next2.Metadata.Width)
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{Width: iptr(4)}})
	next3, _ := mustApply(t, d, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{BackgroundColor: str("#112233")}})
	assert.Equal(t, "#112233", next3.Metadata.BackgroundColor)
	mustFail(t, d, ErrBadRequest, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{BackgroundColor: str("url(evil)")}})
	// 传相同 fps 不算修改
	mustApply(t, d, Operation{Op: OpSetMetadata, Metadata: &MetadataPatch{FPS: iptr(30), Name: str("ok")}})
}

func TestRejectsNonFiniteAudioControls(t *testing.T) {
	d := docWithClip()
	nan := math.NaN()
	inf := math.Inf(1)
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Volume: &nan},
	})
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateItem, ItemID: "it1", Patch: &ItemPatch{Speed: &inf},
	})
	mustFail(t, d, ErrBadRequest, Operation{
		Op: OpUpdateTrack, TrackID: "a1", TrackPatch: &TrackPatch{Volume: &nan},
	})
}

// ---- 批语义与整体校验 ----

func TestBatchAtomicityAndOverlap(t *testing.T) {
	d := docWithClip() // it1: [100, 220)

	// 批内临时重叠、批后已解决 → 成功
	mustApply(t, d,
		Operation{Op: OpAddItem, Item: &Item{ItemID: "it2", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 150, DurationInFrames: 50}},
		Operation{Op: OpMoveItem, ItemID: "it2", From: i64(300)},
	)

	// 批后仍重叠 → 整批失败，文档不变
	err := mustFail(t, d, ErrOverlap,
		Operation{Op: OpAddItem, Item: &Item{ItemID: "it2", TrackID: "v1", Kind: ItemVideo, MediaID: "m_video", From: 150, DurationInFrames: 50}},
	)
	assert.Equal(t, -1, err.Index)

	// 批中第二个 op 失败 → 第一个 op 也不生效
	_, _, aerr := ApplyOps(d, []Operation{
		{Op: OpMoveItem, ItemID: "it1", From: i64(0)},
		{Op: OpRemoveItems, ItemIDs: []string{"nope"}},
	})
	require.Error(t, aerr)
	assert.Equal(t, int64(100), d.Items[0].From)
}

func TestSplitThenEditRightHalf(t *testing.T) {
	// 文本剪视频的典型批：split 出的新 item 在同批内继续被操作
	d := docWithClip()
	next, _ := mustApply(t, d,
		Operation{Op: OpSplit, ItemID: "it1", Frame: i64(150)},
		Operation{Op: OpRemoveItems, ItemIDs: []string{"it1~150"}},
	)
	require.Len(t, next.Items, 1)
	assert.Equal(t, int64(50), next.Items[0].DurationInFrames)
}

func TestValidateDocLimitsAndDuration(t *testing.T) {
	d := docWithClip()
	assert.Equal(t, int64(220), DocDurationInFrames(d))
	require.Nil(t, ValidateDoc(d))

	bad := d.Clone()
	bad.Transitions = []Transition{{TransitionID: "t1"}}
	require.NotNil(t, ValidateDoc(bad))
}

func TestMediaIDValidationPreventsPathTraversal(t *testing.T) {
	d := baseDoc()
	for _, mediaID := range []string{"../escape", "a/b", ".", "", strings.Repeat("a", 129)} {
		_, _, err := ApplyOps(d, []Operation{{Op: OpAddMedia, Media: &Media{
			MediaID: mediaID, URI: "https://example.invalid/media", Kind: MediaImage,
		}}})
		require.Error(t, err, mediaID)
	}
	_, _, err := ApplyOps(d, []Operation{{Op: OpRemoveMedia, MediaID: "../m"}})
	require.Error(t, err)
}

func TestUpdateMediaRejectsStaleGenerationIncludingLegacyZero(t *testing.T) {
	d := baseDoc()
	d.Media = []Media{{
		MediaID: "m1", Generation: 2, URI: "managed", Kind: MediaImage,
	}}
	legacy := uint64(0)
	name := "stale"
	_, _, err := ApplyOps(d, []Operation{{
		Op: OpUpdateMedia, MediaID: "m1", MediaGeneration: &legacy,
		MediaPatch: &MediaPatch{DisplayName: &name},
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generation")
}
