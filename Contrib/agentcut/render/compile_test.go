package render

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/timeline"
)

// fixtureDoc 构造覆盖 MVP 全部路径的文档：
// 底轨（裁剪+间隙+变速）、叠加图片轨、两条音轨（一条 muted）、字幕轨。
func fixtureDoc() timeline.Doc {
	d := timeline.NewDoc("t", 1920, 1080, 30)
	d.Media = []timeline.Media{
		{MediaID: "mv", URI: "https://blob/mv.mp4", Kind: timeline.MediaVideo, DurationInFrames: 1800},
		{MediaID: "mi", URI: "https://blob/mi.png", Kind: timeline.MediaImage},
		{MediaID: "ma", URI: "https://blob/ma.mp3", Kind: timeline.MediaAudio, DurationInFrames: 600},
		{MediaID: "mb", URI: "https://blob/mb.mp3", Kind: timeline.MediaAudio, DurationInFrames: 6000},
	}
	d.Tracks = []timeline.Track{
		{TrackID: "v1", Kind: timeline.TrackVideo},
		{TrackID: "v2", Kind: timeline.TrackVideo},
		{TrackID: "a1", Kind: timeline.TrackAudio, Volume: 0.8},
		{TrackID: "a2", Kind: timeline.TrackAudio, Muted: true},
		{TrackID: "c1", Kind: timeline.TrackCaption},
	}
	d.Items = []timeline.Item{
		// v1：0-300 裁剪过的视频，300-360 间隙，360-600 两倍速视频
		{ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 0, DurationInFrames: 300, TrimStart: 30},
		{ItemID: "i2", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 360, DurationInFrames: 240, Speed: 2},
		// v2：100-220 叠加图片
		{ItemID: "p1", TrackID: "v2", Kind: timeline.ItemImage, MediaID: "mi", From: 100, DurationInFrames: 120},
		// a1：配音 + 带 trimStart 的第二段
		{ItemID: "t1", TrackID: "a1", Kind: timeline.ItemAudio, MediaID: "ma", From: 60, DurationInFrames: 300},
		{ItemID: "t2", TrackID: "a1", Kind: timeline.ItemAudio, MediaID: "mb", From: 400, DurationInFrames: 100, TrimStart: 30},
		// a2（muted）：应被整轨跳过
		{ItemID: "x1", TrackID: "a2", Kind: timeline.ItemAudio, MediaID: "mb", From: 0, DurationInFrames: 100},
		// c1：两条字幕
		{ItemID: "s1", TrackID: "c1", Kind: timeline.ItemText, From: 0, DurationInFrames: 90, Text: "第一句"},
		{ItemID: "s2", TrackID: "c1", Kind: timeline.ItemText, From: 90, DurationInFrames: 90, Text: "第二句"},
	}
	return d
}

func callByID(t *testing.T, p Plan, id string) Call {
	t.Helper()
	for _, c := range p.Calls {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("call %q not found in plan: %v", id, callIDs(p))
	return Call{}
}

func callIDs(p Plan) []string {
	ids := make([]string, len(p.Calls))
	for i, c := range p.Calls {
		ids[i] = c.ID
	}
	return ids
}

func TestCompileFullScenario(t *testing.T) {
	plan, err := Compile(fixtureDoc())
	require.NoError(t, err)

	// 期望的调用顺序（线性依赖链）
	assert.Equal(t, []string{
		"item:i1", "track:v1:gap0", "item:i2", "track:v1:concat",
		"item:p1", "overlay:p1",
		"audio:t1", "audio:t2", "audiomix",
		"captions",
	}, callIDs(plan))

	// i1：clip_av start=1s（30f 裁剪）duration=10s（300f）
	i1 := callByID(t, plan, "item:i1")
	steps := i1.Input["pipeline"].([]map[string]any)
	require.Len(t, steps, 1)
	assert.Equal(t, "clip_av", steps[0]["op"])
	assert.Equal(t, 1.0, steps[0]["params"].(map[string]any)["start"])
	assert.Equal(t, 10.0, steps[0]["params"].(map[string]any)["duration"])
	assert.Equal(t, "https://blob/mv.mp4", i1.Input["video"])

	// i2：两倍速 → clip_av 消耗 480f=16s，retime 到 8s
	i2 := callByID(t, plan, "item:i2")
	steps = i2.Input["pipeline"].([]map[string]any)
	require.Len(t, steps, 2)
	assert.Equal(t, 16.0, steps[0]["params"].(map[string]any)["duration"])
	assert.Equal(t, "retime_av", steps[1]["op"])
	assert.Equal(t, 8.0, steps[1]["params"].(map[string]any)["target_duration"])

	// 间隙：60f = 2s 黑场
	gap := callByID(t, plan, "track:v1:gap0")
	assert.Equal(t, "title_card", gap.Input["op"])
	assert.Equal(t, 2.0, gap.Input["params"].(map[string]any)["duration"])

	// concat 引用三段
	concat := callByID(t, plan, "track:v1:concat")
	assert.Equal(t, []string{
		OutputRef("item:i1"), OutputRef("track:v1:gap0"), OutputRef("item:i2"),
	}, concat.Input["videos"])

	// overlay：全画布，时间窗 100f-220f
	ov := callByID(t, plan, "overlay:p1")
	p := ov.Input["params"].(map[string]any)
	assert.Equal(t, 1920, p["w"])
	assert.InDelta(t, 3.333, p["start"].(float64), 0.001)
	assert.InDelta(t, 7.333, p["end"].(float64), 0.001)
	assert.Equal(t, OutputRef("track:v1:concat"), ov.Input["video"])
	assert.Equal(t, OutputRef("item:p1"), ov.Input["video2"])

	// 混音：a2 整轨 muted 被跳过；offsets/gains 对齐
	mix := callByID(t, plan, "audiomix")
	assert.Equal(t, []string{OutputRef("audio:t1"), OutputRef("audio:t2")}, mix.Input["audios"])
	mp := mix.Input["params"].(map[string]any)
	assert.Equal(t, []int64{2000, 13333}, mp["offsets_ms"])
	assert.Equal(t, []float64{0.8, 0.8}, mp["gains"])
	assert.Equal(t, OutputRef("overlay:p1"), mix.Input["video"])

	// t2 带 trimStart → cut + fit，且有 kess 实测警告
	t2 := callByID(t, plan, "audio:t2")
	steps = t2.Input["pipeline"].([]map[string]any)
	assert.Equal(t, "cut", steps[0]["op"])
	assert.True(t, hasWarning(plan, "trimStart"), "expected cut-on-audio warning")

	// 字幕：base64 SRT，两条都在
	cap := callByID(t, plan, "captions")
	assert.Equal(t, "burn_subtitles", cap.Input["op"])
	srtB64 := strings.TrimPrefix(cap.Input["subtitle"].(string), "base64:")
	srt, err := base64.StdEncoding.DecodeString(srtB64)
	require.NoError(t, err)
	assert.Contains(t, string(srt), "第一句")
	assert.Contains(t, string(srt), "00:00:03,000 --> 00:00:06,000")
	assert.Equal(t, OutputRef("audiomix"), cap.Input["video"])
	assert.True(t, hasWarning(plan, "CJK 字体"))
	assert.True(t, hasWarning(plan, "title_card"))
	for _, warning := range plan.Warnings {
		assert.True(t, strings.HasPrefix(warning, "[KESS] "), warning)
	}
}

func hasWarning(p Plan, substr string) bool {
	for _, w := range p.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestCompileDeterministic(t *testing.T) {
	a, err := Compile(fixtureDoc())
	require.NoError(t, err)
	b, err := Compile(fixtureDoc())
	require.NoError(t, err)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	assert.Equal(t, string(aj), string(bj), "compile must be deterministic")
}

func TestCompileSkipsMutedCaptionTracks(t *testing.T) {
	doc := fixtureDoc()
	for i := range doc.Tracks {
		if doc.Tracks[i].TrackID == "c1" {
			doc.Tracks[i].Muted = true
		}
	}

	plan, err := Compile(doc)
	require.NoError(t, err)
	assert.NotContains(t, callIDs(plan), "captions")
	assert.True(t, hasWarning(plan, "跳过烧录"))
}

func TestCompileManagedUsesLocalWarningsWithoutCJKFalsePositive(t *testing.T) {
	doc := fixtureDoc()
	doc.Items[0].Volume = 0.5

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	assert.Empty(t, plan.Warnings, "supported local semantics must not emit degradation warnings")
	for _, warning := range plan.Warnings {
		assert.True(t, strings.HasPrefix(warning, "[local] "), warning)
		assert.NotContains(t, warning, "CJK 字体")
		assert.NotContains(t, warning, "KESS")
		assert.NotContains(t, warning, "title_card")
		assert.NotContains(t, warning, "trimStart")
	}
	steps := callByID(t, plan, "item:i1").Input["pipeline"].([]map[string]any)
	require.Len(t, steps, 2)
	assert.Equal(t, "apply_av_effects", steps[1]["op"])
	assert.Equal(t, 0.5, steps[1]["params"].(map[string]any)["gain"])
}

func TestCompileManagedHiddenVideoKeepsEmbeddedAudio(t *testing.T) {
	doc := timeline.NewDoc("hidden video", 1280, 720, 30)
	doc.Media = []timeline.Media{{
		MediaID: "mv", URI: "https://blob/video.mp4",
		Kind: timeline.MediaVideo, DurationInFrames: 300,
	}}
	doc.Tracks = []timeline.Track{{
		TrackID: "v1", Kind: timeline.TrackVideo, Hidden: true,
	}}
	doc.Items = []timeline.Item{{
		ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo,
		MediaID: "mv", From: 0, DurationInFrames: 300,
	}}

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"canvas:background", "embedded-audio:i1", "audiomix",
	}, callIDs(plan))
	assert.NotContains(t, callIDs(plan), "item:i1")

	doc.Tracks[0].Muted = true
	mutedPlan, err := CompileManaged(doc)
	require.NoError(t, err)
	assert.Equal(t, []string{"canvas:background"}, callIDs(mutedPlan),
		"muted removes sound while hidden alone must not")
}

func TestCompileEdgeCases(t *testing.T) {
	// 空时间线
	_, err := Compile(timeline.NewDoc("空", 1920, 1080, 30))
	require.Error(t, err)

	// 只有音频没有画面
	d := timeline.NewDoc("音频", 1920, 1080, 30)
	d.Media = []timeline.Media{{MediaID: "ma", URI: "u", Kind: timeline.MediaAudio, DurationInFrames: 100}}
	d.Tracks = []timeline.Track{{TrackID: "a1", Kind: timeline.TrackAudio}}
	d.Items = []timeline.Item{{ItemID: "t1", TrackID: "a1", Kind: timeline.ItemAudio, MediaID: "ma", From: 0, DurationInFrames: 100}}
	_, err = Compile(d)
	require.Error(t, err, "MVP requires at least one visual item")

	// 单片段整轨覆盖 → 不需要 concat
	d2 := timeline.NewDoc("单段", 1280, 720, 30)
	d2.Media = []timeline.Media{{MediaID: "mv", URI: "u", Kind: timeline.MediaVideo, DurationInFrames: 300}}
	d2.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	d2.Items = []timeline.Item{{ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 0, DurationInFrames: 300}}
	plan, err := Compile(d2)
	require.NoError(t, err)
	assert.Equal(t, []string{"item:i1"}, callIDs(plan))

	// Agent-K 不得把转场静默降级为硬切。
	d2.Transitions = []timeline.Transition{{TransitionID: "tr1", FromItemID: "i1", ToItemID: "i1", Kind: "crossfade", Frames: 15}}
	_, err = Compile(d2)
	var unsupported *UnsupportedRenderFeatureError
	require.ErrorAs(t, err, &unsupported)

	// muted 视频轨 → strip_audio + add_silent_audio
	d3 := timeline.NewDoc("静音轨", 1280, 720, 30)
	d3.Media = []timeline.Media{{MediaID: "mv", URI: "u", Kind: timeline.MediaVideo, DurationInFrames: 300}}
	d3.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo, Muted: true}}
	d3.Items = []timeline.Item{{ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 0, DurationInFrames: 300}}
	plan, err = Compile(d3)
	require.NoError(t, err)
	steps := callByID(t, plan, "item:i1").Input["pipeline"].([]map[string]any)
	require.Len(t, steps, 3)
	assert.Equal(t, "strip_audio", steps[1]["op"])
	assert.Equal(t, "add_silent_audio", steps[2]["op"])

	// 引用不存在的素材
	d4 := timeline.NewDoc("坏引用", 1280, 720, 30)
	d4.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	d4.Items = []timeline.Item{{ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "nope", From: 0, DurationInFrames: 300}}
	_, err = Compile(d4)
	require.Error(t, err)
}

func TestCompileManagedAudioSpeedAndFadePlan(t *testing.T) {
	for _, speed := range []float64{0.1, 0.5, 1.5, 2, 4, 10} {
		t.Run(fmt.Sprintf("%.1fx", speed), func(t *testing.T) {
			doc := fixtureDoc()
			doc.Items[3].MediaID = "mb"
			doc.Items[3].DurationInFrames = 30
			doc.Items[3].Speed = speed
			doc.Items[3].FadeInFrames = 3
			doc.Items[3].FadeOutFrames = 6
			plan, err := CompileManaged(doc)
			require.NoError(t, err)
			call := callByID(t, plan, "audio:t1")
			assert.Equal(t, "prepare_audio", call.Input["op"])
			params := call.Input["params"].(map[string]any)
			assert.Equal(t, 1.0, params["target_duration"])
			assert.Equal(t, speed, params["source_duration"])
			assert.Equal(t, 0.1, params["fade_in"])
			assert.Equal(t, 0.2, params["fade_out"])
			assert.False(t, hasWarning(plan, "原速"))
		})
	}
}

func TestCompileManagedExactFadeBoundaryAndTinyGain(t *testing.T) {
	doc := fixtureDoc()
	doc.Items[0].DurationInFrames = 2
	doc.Items[0].FadeInFrames = 1
	doc.Items[0].FadeOutFrames = 1
	doc.Items[3].Volume = 1e-10

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	itemCall := callByID(t, plan, "item:i1")
	_, err = validateCallInput(itemCall.Input)
	require.NoError(t, err, "frame-exact fade sums must survive seconds conversion")
	mix := callByID(t, plan, "audiomix")
	gains := mix.Input["params"].(map[string]any)["gains"].([]float64)
	require.NotEmpty(t, gains)
	assert.InDelta(t, 1e-10*0.8, gains[0], 1e-24,
		"a positive gain must not be treated as muted")
}

func TestCompileManagedVideoAudioGainMuteAndFade(t *testing.T) {
	doc := fixtureDoc()
	doc.Tracks[0].Volume = 0.8
	doc.Items[0].Volume = 0.5
	doc.Items[0].FadeInFrames = 30
	doc.Items[0].FadeOutFrames = 60

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	steps := callByID(t, plan, "item:i1").Input["pipeline"].([]map[string]any)
	effects := steps[len(steps)-1]
	assert.Equal(t, "apply_av_effects", effects["op"])
	params := effects["params"].(map[string]any)
	assert.InDelta(t, 0.4, params["gain"], 1e-9)
	assert.False(t, params["muted"].(bool))
	assert.Equal(t, 1.0, params["fade_in"])
	assert.Equal(t, 2.0, params["fade_out"])
	assert.True(t, params["visual_fade"].(bool))

	doc.Items[0].Muted = true
	doc.Items[0].Volume = 0 // historical unset value remains gain 1
	doc.Tracks[0].Volume = 0
	plan, err = CompileManaged(doc)
	require.NoError(t, err)
	steps = callByID(t, plan, "item:i1").Input["pipeline"].([]map[string]any)
	params = steps[len(steps)-1]["params"].(map[string]any)
	assert.Equal(t, 1.0, params["gain"])
	assert.True(t, params["muted"].(bool))

	doc.Items[0].Muted = false
	doc.Tracks[0].Muted = true
	plan, err = CompileManaged(doc)
	require.NoError(t, err)
	assert.Contains(t, callIDs(plan), "item:i1", "muting a video track must not hide its picture")
	steps = callByID(t, plan, "item:i1").Input["pipeline"].([]map[string]any)
	params = steps[len(steps)-1]["params"].(map[string]any)
	assert.True(t, params["muted"].(bool))
}

func TestCompileManagedPiPBackgroundAndFirstTrackTransform(t *testing.T) {
	doc := fixtureDoc()
	doc.Metadata.BackgroundColor = "#112233"
	doc.Items[0].Transform = &timeline.ItemTransform{
		X: 0.5, Y: 0.25, Width: 0.25, Height: 0.5,
		Opacity: 0.6, Fit: timeline.ItemFitCover,
	}
	doc.Items[0].FadeInFrames = 30

	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	assert.Equal(t, "canvas:background", plan.Calls[0].ID)
	background := callByID(t, plan, "canvas:background")
	assert.Equal(t, "#112233", background.Input["params"].(map[string]any)["bg"])

	item := callByID(t, plan, "item:i1")
	clipParams := item.Input["pipeline"].([]map[string]any)[0]["params"].(map[string]any)
	assert.Equal(t, 480, clipParams["w"])
	assert.Equal(t, 540, clipParams["h"])
	assert.Equal(t, timeline.ItemFitCover, clipParams["fit"])

	overlay := callByID(t, plan, "overlay:i1")
	params := overlay.Input["params"].(map[string]any)
	assert.Equal(t, 960, params["x"])
	assert.Equal(t, 270, params["y"])
	assert.Equal(t, 480, params["w"])
	assert.Equal(t, 540, params["h"])
	assert.Equal(t, 0.6, params["opacity"])
	assert.Equal(t, timeline.ItemFitCover, params["fit"])
	assert.Equal(t, 1.0, params["fade_in"])
	assert.Contains(t, callIDs(plan), "embedded-audio:i1")

	gap := callByID(t, plan, "canvas:background")
	assert.Equal(t, "#112233", gap.Input["params"].(map[string]any)["bg"])
}

func TestTinyNormalizedTransformProducesValidPixels(t *testing.T) {
	doc := fixtureDoc()
	doc.Items[0].Transform = &timeline.ItemTransform{
		X: 0.9999, Y: 0.9999, Width: 0.0001, Height: 0.0001,
	}
	doc.Items[0].Muted = true
	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	overlay := callByID(t, plan, "overlay:i1")
	params := overlay.Input["params"].(map[string]any)
	assert.Equal(t, 1, params["w"])
	assert.Equal(t, 1, params["h"])
	item := callByID(t, plan, "item:i1")
	clipParams := item.Input["pipeline"].([]map[string]any)[0]["params"].(map[string]any)
	assert.Equal(t, 2, clipParams["w"], "intermediate H.264 dimensions must be even")
	assert.Equal(t, 2, clipParams["h"])
	steps := item.Input["pipeline"].([]map[string]any)
	effectParams := steps[len(steps)-1]["params"].(map[string]any)
	assert.Equal(t, 2, effectParams["w"])
	assert.Equal(t, 2, effectParams["h"])
	_, err = validateCallInput(overlay.Input)
	require.NoError(t, err)
}

func TestAgentKRejectsLocalOnlySemantics(t *testing.T) {
	for name, mutate := range map[string]func(*timeline.Doc){
		"audio speed": func(doc *timeline.Doc) { doc.Items[3].Speed = 2 },
		"fade":        func(doc *timeline.Doc) { doc.Items[0].FadeInFrames = 1 },
		"transform": func(doc *timeline.Doc) {
			doc.Items[0].Transform = &timeline.ItemTransform{Width: 1, Height: 1}
		},
		"video gain": func(doc *timeline.Doc) { doc.Items[0].Volume = 0.5 },
		"background": func(doc *timeline.Doc) { doc.Metadata.BackgroundColor = "white" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := fixtureDoc()
			mutate(&doc)
			_, err := Compile(doc)
			var unsupported *UnsupportedRenderFeatureError
			require.True(t, errors.As(err, &unsupported), err)
		})
	}
	doc := fixtureDoc()
	doc.Metadata.BackgroundColor = "#000000"
	_, err := Compile(doc)
	require.NoError(t, err, "hex black is semantically equivalent to named black")
}

func TestResolveRefs(t *testing.T) {
	input := map[string]any{
		"op":     "concat_av",
		"params": map[string]any{"w": 1920},
		"videos": []string{OutputRef("a"), "https://direct.example/x.mp4", OutputRef("b")},
		"video":  OutputRef("c"),
	}
	resolved, ok := ResolveRefs(input, map[string]string{"a": "u-a", "b": "u-b", "c": "u-c"})
	require.True(t, ok)
	assert.Equal(t, []any{"u-a", "https://direct.example/x.mp4", "u-b"}, resolved["videos"])
	assert.Equal(t, "u-c", resolved["video"])
	// 原始 input 不被修改
	assert.Equal(t, OutputRef("c"), input["video"])

	// 未知引用
	_, ok = ResolveRefs(map[string]any{"video": OutputRef("missing")}, map[string]string{})
	assert.False(t, ok)
}

func TestKeyIdempotent(t *testing.T) {
	k1, err := KeyOf(fixtureDoc())
	require.NoError(t, err)
	k2, err := KeyOf(fixtureDoc())
	require.NoError(t, err)
	assert.Equal(t, k1, k2)

	changed := fixtureDoc()
	changed.Items[0].DurationInFrames = 301
	k3, err := KeyOf(changed)
	require.NoError(t, err)
	assert.NotEqual(t, k1, k3)
}

func TestCompileManagedM3BTransitionAndDuckPlan(t *testing.T) {
	doc := timeline.NewDoc("m3b", 320, 180, 24)
	doc.Media = []timeline.Media{
		{MediaID: "v", Kind: timeline.MediaVideo, URI: "/v.mp4", DurationInFrames: 120,
			Probe: &timeline.MediaProbe{HasAudio: true}},
		{MediaID: "voice", Kind: timeline.MediaAudio, URI: "/voice.m4a", DurationInFrames: 120},
		{MediaID: "bed", Kind: timeline.MediaAudio, URI: "/bed.m4a", DurationInFrames: 120},
	}
	doc.Tracks = []timeline.Track{
		{TrackID: "video", Kind: timeline.TrackVideo},
		{TrackID: "voice", Kind: timeline.TrackAudio},
		{TrackID: "bed", Kind: timeline.TrackAudio, Ducking: &timeline.DuckingConfig{
			SidechainTrackIDs: []string{"voice"}, Gain: 0.25,
			AttackFrames: 3, ReleaseFrames: 6,
		}},
	}
	doc.Items = []timeline.Item{
		{ItemID: "from", TrackID: "video", Kind: timeline.ItemVideo, MediaID: "v", From: 0, DurationInFrames: 24},
		{ItemID: "to", TrackID: "video", Kind: timeline.ItemVideo, MediaID: "v", From: 24, DurationInFrames: 24, TrimStart: 48},
		{ItemID: "voice", TrackID: "voice", Kind: timeline.ItemAudio, MediaID: "voice", From: 18, DurationInFrames: 12},
		{ItemID: "bed", TrackID: "bed", Kind: timeline.ItemAudio, MediaID: "bed", From: 0, DurationInFrames: 48},
	}
	doc.Transitions = []timeline.Transition{{
		TransitionID: "tr", FromItemID: "from", ToItemID: "to",
		Kind: timeline.TransitionCrossfade, Frames: 6,
	}}

	first, err := CompileManaged(doc)
	require.NoError(t, err)
	second, err := CompileManaged(doc)
	require.NoError(t, err)
	assert.Equal(t, first, second, "M3B compilation must remain deterministic")
	var operations []string
	var mixParams map[string]any
	for _, call := range first.Calls {
		if operation, ok := call.Input["op"].(string); ok {
			operations = append(operations, operation)
			if operation == "mix_audio" {
				mixParams, _ = call.Input["params"].(map[string]any)
			}
		}
	}
	assert.Contains(t, operations, "transition_overlay")
	assert.Contains(t, operations, "transition_audio")
	require.NotNil(t, mixParams)
	assert.Contains(t, mixParams, "envelopes")
	assert.Empty(t, first.Warnings)

	doc.Transitions[0].Kind = timeline.TransitionDipToBlack
	dipPlan, err := CompileManaged(doc)
	require.NoError(t, err)
	dipCall := callByID(t, dipPlan, "transition:tr:overlay")
	dipParams := dipCall.Input["params"].(map[string]any)
	assert.Equal(t, timeline.TransitionDipToBlack, dipParams["kind"])
}

func TestAgentKRejectsEveryM3BFeature(t *testing.T) {
	cases := map[string]func(*timeline.Doc){
		"transition": func(doc *timeline.Doc) {
			doc.Transitions = []timeline.Transition{{
				TransitionID: "t", FromItemID: "v1", ToItemID: "v2",
				Kind: timeline.TransitionCrossfade, Frames: 1,
			}}
		},
		"ducking": func(doc *timeline.Doc) {
			doc.Tracks = append(doc.Tracks, timeline.Track{
				TrackID: "duck", Kind: timeline.TrackAudio,
				Ducking: &timeline.DuckingConfig{
					SidechainTrackIDs: []string{"a1"}, Gain: 0.5,
				},
			})
		},
		"caption style": func(doc *timeline.Doc) {
			value := true
			doc.Items = append(doc.Items, timeline.Item{
				ItemID: "caption", TrackID: "c1", Kind: timeline.ItemText,
				From: 0, DurationInFrames: 10, Text: "styled",
				CaptionStyle: &timeline.CaptionStyle{Bold: &value},
			})
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := fixtureDoc()
			mutate(&doc)
			_, err := Compile(doc)
			var unsupported *UnsupportedRenderFeatureError
			require.ErrorAs(t, err, &unsupported)
		})
	}
}
