package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/timeline"
)

func requireRealFFmpeg(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("ffmpeg not found in PATH")
	}
	require.NoError(t, err)
	ffprobe, err := exec.LookPath("ffprobe")
	if errors.Is(err, exec.ErrNotFound) {
		t.Skip("ffprobe not found in PATH")
	}
	require.NoError(t, err)
	return ffmpeg, ffprobe
}

func TestLocalActivityRealFFmpegFullCompilerPlan(t *testing.T) {
	ffmpeg, ffprobe := requireRealFFmpeg(t)
	ctx := context.Background()
	files := t.TempDir()
	videoPath := filepath.Join(files, "source.mp4")
	audioPath := filepath.Join(files, "source.m4a")
	imagePath := filepath.Join(files, "source.png")

	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=blue:s=160x90:r=24:d=4",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=4",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a", "aac", videoPath))
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=4",
		"-vn", "-c:a", "aac", audioPath))
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:s=64x64:d=0.1",
		"-frames:v", "1", "-update", "1", imagePath))

	store, err := blob.NewLocalStore(filepath.Join(files, "blob"))
	require.NoError(t, err)
	projectID := "p1"
	renderKey := "0123456789abcdef"
	videoKey := blob.SourceKey(projectID, "mv", "source.mp4")
	audioKey := blob.SourceKey(projectID, "ma", "source.m4a")
	imageKey := blob.SourceKey(projectID, "mi", "source.png")
	putFile(t, store, videoKey, videoPath, "video/mp4")
	putFile(t, store, audioKey, audioPath, "audio/mp4")
	putFile(t, store, imageKey, imagePath, "image/png")

	doc := timeline.NewDoc("integration", 320, 180, 24)
	doc.Metadata.BackgroundColor = "#00ff00"
	doc.Media = []timeline.Media{
		{MediaID: "mv", URI: "/ignored/video", BlobKey: videoKey, Kind: timeline.MediaVideo, DurationInFrames: 96},
		{MediaID: "ma", URI: "/ignored/audio", BlobKey: audioKey, Kind: timeline.MediaAudio, DurationInFrames: 96},
		{MediaID: "mi", URI: "/ignored/image", BlobKey: imageKey, Kind: timeline.MediaImage},
	}
	doc.Tracks = []timeline.Track{
		{TrackID: "v1", Kind: timeline.TrackVideo, Muted: true},
		{TrackID: "v2", Kind: timeline.TrackVideo},
		{TrackID: "a1", Kind: timeline.TrackAudio, Volume: 0.5},
		{TrackID: "c1", Kind: timeline.TrackCaption},
	}
	doc.Items = []timeline.Item{
		{ItemID: "i1", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 0, DurationInFrames: 24,
			FadeInFrames: 6,
			Transform:    &timeline.ItemTransform{X: 0, Y: 0, Width: 0.5, Height: 0.5, Fit: timeline.ItemFitCover}},
		{ItemID: "i2", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "mv", From: 36, DurationInFrames: 24, Speed: 1.5},
		{ItemID: "p1", TrackID: "v2", Kind: timeline.ItemImage, MediaID: "mi", From: 12, DurationInFrames: 24,
			Transform: &timeline.ItemTransform{
				X: 0.5, Y: 0, Width: 0.5, Height: 0.5, Opacity: 0.5, Fit: timeline.ItemFitCover,
				Crop:            &timeline.NormalizedRect{X: 0.1, Y: 0.1, Width: 0.8, Height: 0.8},
				RotationDegrees: 45,
			}},
		{ItemID: "a1", TrackID: "a1", Kind: timeline.ItemAudio, MediaID: "ma", From: 0, DurationInFrames: 24,
			Speed: 2, FadeInFrames: 6, FadeOutFrames: 6},
		{ItemID: "a2", TrackID: "a1", Kind: timeline.ItemAudio, MediaID: "ma", From: 24, DurationInFrames: 24, TrimStart: 12},
		{ItemID: "s1", TrackID: "c1", Kind: timeline.ItemText, From: 0, DurationInFrames: 48,
			Text: "你好，世界\n复杂字幕", StylePresetID: "subtitle-outline"},
	}
	plan, err := CompileManaged(doc)
	require.NoError(t, err)
	assertPlanCoversEveryLocalOperation(t, plan)

	activities := &LocalActivities{
		Store: store, FFmpegBinary: ffmpeg, FFprobeBinary: ffprobe,
		Timeout: 2 * time.Minute, TempRoot: files,
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(activities.Execute, activity.RegisterOptions{Name: LocalActivityName})
	outputs := make(map[string]string)
	for i, call := range plan.Calls {
		resolved, ok := ResolveRefs(call.Input, outputs)
		require.True(t, ok)
		value, err := env.ExecuteActivity(LocalActivityName, LocalActivityInput{
			ProjectID: projectID, RenderKey: renderKey, CallID: call.ID,
			Input: resolved, Media: doc.Media,
			Width: doc.Metadata.Width, Height: doc.Metadata.Height, FPS: doc.Metadata.FPS,
			Final: i == len(plan.Calls)-1,
		})
		require.NoError(t, err, "call %s", call.ID)
		var out LocalActivityOutput
		require.NoError(t, value.Get(&out))
		outputs[call.ID] = out.Key
	}

	finalKey := blob.OutputKey(projectID, renderKey)
	assert.Equal(t, finalKey, outputs["captions"])
	finalPath := copyObject(t, store, finalKey, filepath.Join(files, "final.mp4"))
	probe, err := (platformmedia.FFProber{Binary: ffprobe, Timeout: time.Minute}).Probe(ctx, finalPath)
	require.NoError(t, err)
	assert.Equal(t, 320, probe.Width)
	assert.Equal(t, 180, probe.Height)
	assert.InDelta(t, 24, probe.NativeFPS, 0.01)
	assert.InDelta(t, 2500, probe.DurationMs, 150)
	assert.True(t, probe.HasAudio)
	rms := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "audio.pcm"), "0.400")
	assert.Greater(t, rms, 500.0)
	assert.Less(t, rms, 2200.0,
		"track volume 0.5 should reduce the default lavfi sine RMS")
	fadeRMS := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "audio-fade.pcm"), "0.025")
	assert.Less(t, fadeRMS, rms*0.75, "audio fade-in should reduce early samples")
	t.Logf("ffprobe integration result: width=%d height=%d fps=%.3f durationMs=%d hasAudio=%t codec=%s",
		probe.Width, probe.Height, probe.NativeFPS, probe.DurationMs, probe.HasAudio, probe.Codec)

	preCaptionPath := copyObject(t, store, outputs["audiomix"], filepath.Join(files, "pre-caption.mp4"))
	before := extractRGBFrame(t, ffmpeg, preCaptionPath, filepath.Join(files, "before.rgb"))
	after := extractRGBFrame(t, ffmpeg, finalPath, filepath.Join(files, "after.rgb"))
	require.Equal(t, len(before), len(after))
	topDiff, bottomDiff := frameDiffHalves(before, after, 320, 180)
	t.Logf("Chinese subtitle pixel diff: top=%d bottom=%d", topDiff, bottomDiff)
	assert.Greater(t, bottomDiff, topDiff+100,
		"Chinese subtitle should add visible pixels in the lower half")

	pipFrame := extractRGBFrameAt(t, ffmpeg, preCaptionPath, filepath.Join(files, "pip.rgb"), "0.750")
	outside := rgbAt(pipFrame, 320, 40, 40)
	inside := rgbAt(pipFrame, 320, 240, 40)
	rotatedCorner := rgbAt(pipFrame, 320, 162, 2)
	assert.Greater(t, outside[2], outside[0]+80, "transformed first video track should render in its region")
	assert.Greater(t, inside[0], outside[0]+40, "semi-transparent red PiP should affect its region")
	assert.Greater(t, rotatedCorner[1], outside[1]+40,
		"transparent rotated corners must be synthesized with metadata background, not black")
	fadeFrame := extractRGBFrameAt(t, ffmpeg, preCaptionPath, filepath.Join(files, "video-fade.rgb"), "0.025")
	fadePixel := rgbAt(fadeFrame, 320, 40, 40)
	assert.Less(t, fadePixel[2], outside[2]-40, "video fade-in should affect transformed visuals")

	backgroundFrame := extractRGBFrameAt(t, ffmpeg, preCaptionPath, filepath.Join(files, "background.rgb"), "1.250")
	backgroundPixel := rgbAt(backgroundFrame, 320, 40, 40)
	assert.Greater(t, backgroundPixel[1], backgroundPixel[0]+80,
		"metadata.backgroundColor should fill base-track gaps")
}

func TestLocalActivityM3BTransitionPlan(t *testing.T) {
	ffmpeg, ffprobe := requireRealFFmpeg(t)
	ctx := context.Background()
	files := t.TempDir()
	fromPath := filepath.Join(files, "from.mp4")
	toPath := filepath.Join(files, "to.mp4")
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=blue:s=160x90:r=24:d=2",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=2",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", fromPath))
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=red:s=160x90:r=24:d=2",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=2",
		"-shortest", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", toPath))

	store, err := blob.NewLocalStore(filepath.Join(files, "blob"))
	require.NoError(t, err)
	const projectID = "transition"
	const renderKey = "1111111111111111"
	fromKey := blob.SourceKey(projectID, "from", "from.mp4")
	toKey := blob.SourceKey(projectID, "to", "to.mp4")
	putFile(t, store, fromKey, fromPath, "video/mp4")
	putFile(t, store, toKey, toPath, "video/mp4")

	doc := timeline.NewDoc("transition", 160, 90, 24)
	doc.Media = []timeline.Media{
		{MediaID: "from", BlobKey: fromKey, Kind: timeline.MediaVideo, DurationInFrames: 48,
			Probe: &timeline.MediaProbe{HasAudio: true}},
		{MediaID: "to", BlobKey: toKey, Kind: timeline.MediaVideo, DurationInFrames: 48,
			Probe: &timeline.MediaProbe{HasAudio: true}},
	}
	doc.Tracks = []timeline.Track{{TrackID: "v", Kind: timeline.TrackVideo}}
	doc.Items = []timeline.Item{
		{ItemID: "from", TrackID: "v", Kind: timeline.ItemVideo, MediaID: "from",
			From: 0, DurationInFrames: 24, FadeOutFrames: 12},
		{ItemID: "to", TrackID: "v", Kind: timeline.ItemVideo, MediaID: "to",
			From: 24, DurationInFrames: 24, FadeInFrames: 12},
	}
	doc.Transitions = []timeline.Transition{{
		TransitionID: "cross", FromItemID: "from", ToItemID: "to",
		Kind: timeline.TransitionCrossfade, Frames: 6,
	}}
	require.Nil(t, timeline.ValidateDoc(doc))
	plan, err := CompileManaged(doc)
	require.NoError(t, err)

	activities := &LocalActivities{
		Store: store, FFmpegBinary: ffmpeg, FFprobeBinary: ffprobe,
		Timeout: 2 * time.Minute, TempRoot: files,
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(activities.Execute, activity.RegisterOptions{Name: LocalActivityName})
	outputs := make(map[string]string)
	for index, call := range plan.Calls {
		resolved, ok := ResolveRefs(call.Input, outputs)
		require.True(t, ok)
		value, executeErr := env.ExecuteActivity(LocalActivityName, LocalActivityInput{
			ProjectID: projectID, RenderKey: renderKey, CallID: call.ID,
			Input: resolved, Media: doc.Media,
			Width: doc.Metadata.Width, Height: doc.Metadata.Height, FPS: doc.Metadata.FPS,
			Final: index == len(plan.Calls)-1,
		})
		require.NoError(t, executeErr, "call %s", call.ID)
		var out LocalActivityOutput
		require.NoError(t, value.Get(&out))
		outputs[call.ID] = out.Key
	}
	finalCall := plan.Calls[len(plan.Calls)-1].ID
	finalPath := copyObject(t, store, outputs[finalCall], filepath.Join(files, "transition-final.mp4"))
	probe, err := (platformmedia.FFProber{Binary: ffprobe, Timeout: time.Minute}).Probe(ctx, finalPath)
	require.NoError(t, err)
	assert.InDelta(t, 2000, probe.DurationMs, 150)
	assert.True(t, probe.HasAudio)

	before := rgbAt(extractRGBFrameAt(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-before.rgb"), "0.950"), 160, 80, 45)
	early := rgbAt(extractRGBFrameAt(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-early.rgb"), "1.020"), 160, 80, 45)
	middle := rgbAt(extractRGBFrameAt(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-middle.rgb"), "1.125"), 160, 80, 45)
	late := rgbAt(extractRGBFrameAt(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-late.rgb"), "1.230"), 160, 80, 45)
	after := rgbAt(extractRGBFrameAt(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-after.rgb"), "1.270"), 160, 80, 45)
	assert.Greater(t, before[2], before[0]+80,
		"outgoing fadeOut must not dim the frame before a transition")
	assert.Greater(t, early[2], early[0], "transition should start from the blue handle")
	assert.Greater(t, late[0], late[2], "transition should finish on the red destination")
	assert.Greater(t, after[0], after[2]+80,
		"incoming fadeIn must not restart after a transition")
	assert.Greater(t, middle[0], early[0], "midpoint should contain destination red")
	assert.Greater(t, middle[2], late[2], "midpoint should retain source blue")
	rms := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "transition.pcm"), "1.125")
	assert.Greater(t, rms, 300.0, "embedded audio must remain present through transition")
	steadyRMS := extractAudioRMS(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-steady.pcm"), "0.300")
	boundaryRMS := extractAudioRMS(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-boundary.pcm"), "0.850")
	assert.Greater(t, boundaryRMS, steadyRMS*0.8,
		"outgoing embedded audio fadeOut must not run before a transition")
	afterRMS := extractAudioRMS(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-after.pcm"), "1.270")
	destinationRMS := extractAudioRMS(t, ffmpeg, finalPath,
		filepath.Join(files, "transition-destination.pcm"), "1.700")
	assert.Greater(t, afterRMS, destinationRMS*0.8,
		"incoming embedded audio fadeIn must not restart after a transition")
}

func TestLocalActivityM3BDuckingRMS(t *testing.T) {
	ffmpeg, ffprobe := requireRealFFmpeg(t)
	ctx := context.Background()
	files := t.TempDir()
	videoPath := filepath.Join(files, "duck-video.mp4")
	bedPath := filepath.Join(files, "duck-bed.m4a")
	silentPath := filepath.Join(files, "duck-silent.m4a")
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=160x90:r=24:d=3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", videoPath))
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3",
		"-vn", "-c:a", "aac", bedPath))
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-t", "1", "-vn", "-c:a", "aac", silentPath))

	store, err := blob.NewLocalStore(filepath.Join(files, "blob"))
	require.NoError(t, err)
	const projectID = "ducking"
	const renderKey = "2222222222222222"
	videoKey := blob.SourceKey(projectID, "video", "video.mp4")
	bedKey := blob.SourceKey(projectID, "bed", "bed.m4a")
	silentKey := blob.SourceKey(projectID, "voice", "voice.m4a")
	putFile(t, store, videoKey, videoPath, "video/mp4")
	putFile(t, store, bedKey, bedPath, "audio/mp4")
	putFile(t, store, silentKey, silentPath, "audio/mp4")

	doc := timeline.NewDoc("duck", 160, 90, 24)
	doc.Media = []timeline.Media{
		{MediaID: "video", BlobKey: videoKey, Kind: timeline.MediaVideo, DurationInFrames: 72},
		{MediaID: "bed", BlobKey: bedKey, Kind: timeline.MediaAudio, DurationInFrames: 72},
		{MediaID: "voice", BlobKey: silentKey, Kind: timeline.MediaAudio, DurationInFrames: 24},
	}
	doc.Tracks = []timeline.Track{
		{TrackID: "video", Kind: timeline.TrackVideo},
		{TrackID: "bed", Kind: timeline.TrackAudio, Ducking: &timeline.DuckingConfig{
			SidechainTrackIDs: []string{"voice"}, Gain: 0.2,
			AttackFrames: 6, ReleaseFrames: 6,
		}},
		{TrackID: "voice", Kind: timeline.TrackAudio},
	}
	doc.Items = []timeline.Item{
		{ItemID: "video", TrackID: "video", Kind: timeline.ItemVideo, MediaID: "video", From: 0, DurationInFrames: 72},
		{ItemID: "bed", TrackID: "bed", Kind: timeline.ItemAudio, MediaID: "bed", From: 0, DurationInFrames: 72},
		{ItemID: "voice", TrackID: "voice", Kind: timeline.ItemAudio, MediaID: "voice", From: 24, DurationInFrames: 24},
	}
	require.Nil(t, timeline.ValidateDoc(doc))
	plan, err := CompileManaged(doc)
	require.NoError(t, err)

	activities := &LocalActivities{
		Store: store, FFmpegBinary: ffmpeg, FFprobeBinary: ffprobe,
		Timeout: 2 * time.Minute, TempRoot: files,
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(activities.Execute, activity.RegisterOptions{Name: LocalActivityName})
	outputs := make(map[string]string)
	for index, call := range plan.Calls {
		resolved, ok := ResolveRefs(call.Input, outputs)
		require.True(t, ok)
		value, executeErr := env.ExecuteActivity(LocalActivityName, LocalActivityInput{
			ProjectID: projectID, RenderKey: renderKey, CallID: call.ID,
			Input: resolved, Media: doc.Media,
			Width: doc.Metadata.Width, Height: doc.Metadata.Height, FPS: doc.Metadata.FPS,
			Final: index == len(plan.Calls)-1,
		})
		require.NoError(t, executeErr, "call %s", call.ID)
		var out LocalActivityOutput
		require.NoError(t, value.Get(&out))
		outputs[call.ID] = out.Key
	}
	finalPath := copyObject(t, store, outputs[plan.Calls[len(plan.Calls)-1].ID],
		filepath.Join(files, "duck-final.mp4"))
	before := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "before.pcm"), "0.400")
	during := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "during.pcm"), "1.500")
	after := extractAudioRMS(t, ffmpeg, finalPath, filepath.Join(files, "after.pcm"), "2.700")
	assert.Less(t, during, before*0.4, "duck gain must lower the bed during sidechain activity")
	assert.InDelta(t, before, after, before*0.25, "release must restore the bed gain")
}

func TestLocalActivityRejectsCrossProjectTimelineSource(t *testing.T) {
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	activities := &LocalActivities{Store: store}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(activities.Execute, activity.RegisterOptions{Name: LocalActivityName})
	_, err = env.ExecuteActivity(LocalActivityName, LocalActivityInput{
		ProjectID: "p1", RenderKey: "0123456789abcdef", CallID: "item:i1",
		Width: 320, Height: 180, FPS: 24,
		Media: []timeline.Media{{
			MediaID: "m1", URI: "/ignored",
			BlobKey: "agentcut/p2/source/m1/stolen.mp4", Kind: timeline.MediaVideo,
		}},
		Input: map[string]any{
			"op": "clip_av", "params": map[string]any{"start": 0.0, "duration": 1.0},
			"video": MediaRef("m1"),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RENDER_SOURCE_REJECTED")
}

func TestLocalPrepareAudioSpeedDurationExtremes(t *testing.T) {
	ffmpeg, ffprobe := requireRealFFmpeg(t)
	ctx := context.Background()
	files := t.TempDir()
	sourcePath := filepath.Join(files, "speed-source.m4a")
	require.NoError(t, platformmedia.RunFFmpeg(ctx, ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=6",
		"-vn", "-c:a", "aac", sourcePath))

	store, err := blob.NewLocalStore(filepath.Join(files, "blob"))
	require.NoError(t, err)
	projectID, renderKey := "p1", "fedcba9876543210"
	sourceKey := blob.SourceKey(projectID, "ma", "speed-source.m4a")
	putFile(t, store, sourceKey, sourcePath, "audio/mp4")
	activities := &LocalActivities{
		Store: store, FFmpegBinary: ffmpeg, FFprobeBinary: ffprobe,
		Timeout: time.Minute, TempRoot: files,
	}
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivityWithOptions(activities.Execute, activity.RegisterOptions{Name: LocalActivityName})

	const targetDuration = 0.5
	for _, speed := range []float64{0.1, 0.5, 1.5, 2, 4, 10} {
		t.Run(fmt.Sprintf("%.1fx", speed), func(t *testing.T) {
			callID := fmt.Sprintf("speed-%02d", int(speed*10))
			value, err := env.ExecuteActivity(LocalActivityName, LocalActivityInput{
				ProjectID: projectID, RenderKey: renderKey, CallID: callID,
				Width: 320, Height: 180, FPS: 24,
				Media: []timeline.Media{{
					MediaID: "ma", BlobKey: sourceKey, Kind: timeline.MediaAudio,
				}},
				Input: map[string]any{
					"op": "prepare_audio",
					"params": map[string]any{
						"start": 0.0, "source_duration": targetDuration * speed,
						"target_duration": targetDuration, "fade_in": 0.05, "fade_out": 0.05,
					},
					"audio": MediaRef("ma"),
				},
			})
			require.NoError(t, err)
			var output LocalActivityOutput
			require.NoError(t, value.Get(&output))
			path := copyObject(t, store, output.Key, filepath.Join(files, callID+".m4a"))
			probe, err := (platformmedia.FFProber{Binary: ffprobe, Timeout: time.Minute}).Probe(ctx, path)
			require.NoError(t, err)
			assert.InDelta(t, 500, probe.DurationMs, 100)
		})
	}
}

func TestIntermediateAndFinalKeysAreDeterministic(t *testing.T) {
	first := IntermediateKey("p1", "0123456789abcdef", "item:a/b", ".mp4")
	second := IntermediateKey("p1", "0123456789abcdef", "item:a/b", ".mp4")
	collisionCandidate := IntermediateKey("p1", "0123456789abcdef", "item:a_b", ".mp4")
	assert.Equal(t, first, second)
	assert.NotEqual(t, first, collisionCandidate)
	assert.Equal(t,
		"agentcut/p1/output/0123456789abcdef.mp4",
		blob.OutputKey("p1", "0123456789abcdef"))
	assert.Contains(t, first, "agentcut/p1/renders/0123456789abcdef/intermediate/")
}

func TestLocalOperationWhitelistAndSubtitleCap(t *testing.T) {
	_, err := validateCallInput(map[string]any{
		"op": "shell", "params": map[string]any{}, "video": "x",
	})
	require.Error(t, err)
	_, err = validateCallInput(map[string]any{
		"op": "clip_av", "params": map[string]any{
			"start": 0.0, "duration": 1.0, "command": "rm -rf /",
		}, "video": "x",
	})
	require.Error(t, err)
	_, err = (&localRunner{tempDir: t.TempDir(), request: LocalActivityInput{Width: 320, Height: 180, FPS: 24}}).
		argsSubtitles(context.Background(), "video.mp4",
			"base64:"+base64Of(bytes.Repeat([]byte("x"), int(maxSubtitleBytes+1))), "out.mp4")
	require.Error(t, err)
}

func assertPlanCoversEveryLocalOperation(t *testing.T, plan Plan) {
	t.Helper()
	seen := map[string]bool{}
	for _, call := range plan.Calls {
		if op, ok := call.Input["op"].(string); ok {
			seen[op] = true
		}
		if raw, ok := call.Input["pipeline"]; ok {
			steps, err := mapSlice(raw)
			require.NoError(t, err)
			for _, step := range steps {
				seen[step["op"].(string)] = true
			}
		}
	}
	for _, op := range []string{
		"clip_av", "retime_av", "title_card", "freeze",
		"apply_av_effects", "overlay_region", "prepare_audio",
		"mix_audio", "burn_subtitles",
	} {
		assert.True(t, seen[op], "operation %s is not covered", op)
	}
}

func putFile(t *testing.T, store blob.Storage, key, path, contentType string) {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()
	info, err := file.Stat()
	require.NoError(t, err)
	_, err = store.Put(context.Background(), key, file, info.Size(), contentType)
	require.NoError(t, err)
}

func copyObject(t *testing.T, store blob.Storage, key, destination string) string {
	t.Helper()
	object, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	defer object.Body.Close()
	file, err := os.Create(destination)
	require.NoError(t, err)
	_, err = io.Copy(file, object.Body)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return destination
}

func extractRGBFrame(t *testing.T, ffmpeg, input, output string) []byte {
	return extractRGBFrameAt(t, ffmpeg, input, output, "0.250")
}

func extractRGBFrameAt(t *testing.T, ffmpeg, input, output, at string) []byte {
	t.Helper()
	require.NoError(t, platformmedia.RunFFmpeg(context.Background(), ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-ss", at, "-i", input, "-frames:v", "1",
		"-f", "rawvideo", "-pix_fmt", "rgb24", output))
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	return raw
}

func rgbAt(frame []byte, width, x, y int) [3]int {
	index := (y*width + x) * 3
	return [3]int{int(frame[index]), int(frame[index+1]), int(frame[index+2])}
}

func extractAudioRMS(t *testing.T, ffmpeg, input, output, at string) float64 {
	t.Helper()
	require.NoError(t, platformmedia.RunFFmpeg(context.Background(), ffmpeg, time.Minute,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-ss", at, "-i", input, "-t", "0.100", "-vn",
		"-f", "s16le", "-ac", "1", "-ar", "48000", output))
	raw, err := os.ReadFile(output)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(raw), 2)
	var sum float64
	samples := len(raw) / 2
	for index := 0; index < samples; index++ {
		value := float64(int16(binary.LittleEndian.Uint16(raw[index*2:])))
		sum += value * value
	}
	return math.Sqrt(sum / float64(samples))
}

func frameDiffHalves(before, after []byte, width, height int) (top, bottom int) {
	rowBytes := width * 3
	for y := 0; y < height; y++ {
		for x := 0; x < rowBytes; x++ {
			index := y*rowBytes + x
			if before[index] == after[index] {
				continue
			}
			if y < height/2 {
				top++
			} else {
				bottom++
			}
		}
	}
	return top, bottom
}

func base64Of(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}
