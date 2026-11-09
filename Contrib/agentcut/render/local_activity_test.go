package render

import (
	"bytes"
	"context"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/platform/blob"
)

func TestLocalCleanupRemovesIntermediatesAndKeepsFinalOutput(t *testing.T) {
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	ctx := context.Background()
	projectID := "project_1"
	renderKey := "0123456789abcdef"
	callIDs := []string{"item:video", "audio:mix"}

	for _, callID := range callIDs {
		for _, ext := range []string{".mp4", ".m4a"} {
			key := IntermediateKey(projectID, renderKey, callID, ext)
			_, err = store.Put(ctx, key, bytes.NewReader([]byte("x")), 1, "application/octet-stream")
			require.NoError(t, err)
		}
	}
	outputKey := blob.OutputKey(projectID, renderKey)
	_, err = store.Put(ctx, outputKey, bytes.NewReader([]byte("final")), 5, "video/mp4")
	require.NoError(t, err)

	activities := &LocalActivities{Store: store}
	require.NoError(t, activities.Cleanup(ctx, LocalCleanupInput{
		ProjectID: projectID,
		RenderKey: renderKey,
		CallIDs:   callIDs,
	}))
	// Cleanup is idempotent so Temporal retries are safe.
	require.NoError(t, activities.Cleanup(ctx, LocalCleanupInput{
		ProjectID: projectID,
		RenderKey: renderKey,
		CallIDs:   callIDs,
	}))

	for _, callID := range callIDs {
		for _, ext := range []string{".mp4", ".m4a"} {
			_, err = store.Get(ctx, IntermediateKey(projectID, renderKey, callID, ext))
			assert.ErrorIs(t, err, os.ErrNotExist)
		}
	}
	output, err := store.Get(ctx, outputKey)
	require.NoError(t, err)
	require.NoError(t, output.Body.Close())
}

func TestAtempoFilterSupportsFullTimelineSpeedRange(t *testing.T) {
	for _, speed := range []float64{0.1, 0.5, 1, 1.5, 2, 4, 10} {
		filter := atempoFilter(speed)
		product := 1.0
		for _, component := range strings.Split(filter, ",") {
			value, err := strconv.ParseFloat(strings.TrimPrefix(component, "atempo="), 64)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, value, 0.5)
			assert.LessOrEqual(t, value, 2.0)
			product *= value
		}
		assert.Less(t, math.Abs(product-speed), 1e-8, filter)
	}
}

func TestOddCanvasUsesCompatiblePixelFormat(t *testing.T) {
	runner := &localRunner{request: LocalActivityInput{Width: 321, Height: 181, FPS: 24}}
	assert.Contains(t, runner.videoCodecs(), "yuv444p")
	runner.request.Width = 320
	runner.request.Height = 180
	assert.Contains(t, runner.videoCodecs(), "yuv420p")
}

func TestLocalM3AOperationValidation(t *testing.T) {
	_, err := validateCallInput(map[string]any{
		"op": "prepare_audio",
		"params": map[string]any{
			"start": 0.0, "source_duration": 10.0, "target_duration": 1.0,
			"fade_in": 0.1, "fade_out": 0.2,
		},
		"audio": "managed-source",
	})
	require.NoError(t, err)

	_, err = validateCallInput(map[string]any{
		"op": "overlay_region",
		"params": map[string]any{
			"x": 10, "y": 10, "w": 100, "h": 80,
			"start": 0.0, "end": 1.0, "opacity": 0.5,
			"fit": "cover", "bg": "#112233", "fade_in": 0.1, "fade_out": 0.2,
		},
		"video": "base", "video2": "upper",
	})
	require.NoError(t, err)
	assert.Equal(t, "1e-10", filterNumber(1e-10),
		"positive gains must not be rounded to the mute value")
}

func TestLocalM3BOperationValidationAndEnvelope(t *testing.T) {
	_, err := validateCallInput(map[string]any{
		"op": "clip_av",
		"params": map[string]any{
			"start": 0.0, "duration": 1.0,
			"w": 320, "h": 180, "fit": "cover", "bg": "#112233",
			"crop_x": 0.1, "crop_y": 0.2, "crop_width": 0.5, "crop_height": 0.6,
			"rotation_degrees": 45.0,
		},
		"video": "source",
	})
	require.NoError(t, err)

	_, err = validateCallInput(map[string]any{
		"op": "transition_overlay",
		"params": map[string]any{
			"kind": "dipToBlack", "start": 1.0, "duration": 0.5,
			"from_x": 0, "from_y": 0, "from_w": 320, "from_h": 180, "from_opacity": 1.0,
			"to_x": 20, "to_y": 10, "to_w": 160, "to_h": 90, "to_opacity": 0.8,
		},
		"video": "base", "video2": "from", "video3": "to",
	})
	require.NoError(t, err)

	_, err = validateCallInput(map[string]any{
		"op": "mix_audio",
		"params": map[string]any{
			"offsets_ms": []int64{0}, "gains": []float64{0.5},
			"envelopes": [][]map[string]any{{
				{"time": 0.0, "gain": 1.0},
				{"time": 0.25, "gain": 0.2},
				{"time": 1.0, "gain": 0.2},
				{"time": 1.25, "gain": 1.0},
			}},
		},
		"video": "base", "audios": []string{"bed"},
	})
	require.NoError(t, err)
	expression := audioEnvelopeExpression([]map[string]any{
		{"time": 0.0, "gain": 1.0},
		{"time": 0.25, "gain": 0.2},
		{"time": 1.0, "gain": 0.2},
	})
	assert.Contains(t, expression, "if(lt(t,0.25)")
	assert.Contains(t, expression, "0.2")
	stepExpression := audioEnvelopeExpression([]map[string]any{
		{"time": 0.0, "gain": 1.0},
		{"time": 1.0, "gain": 0.2, "step": true},
		{"time": 2.0, "gain": 1.0, "step": true},
	})
	assert.NotContains(t, stepExpression, "(t-0)/1")
	assert.Contains(t, stepExpression, "if(lt(t,1),1,")

	_, err = validateCallInput(map[string]any{
		"op": "clip_av",
		"params": map[string]any{
			"start": 0.0, "duration": 1.0,
			"crop_x": 0.8, "crop_y": 0.0, "crop_width": 0.3, "crop_height": 1.0,
		},
		"video": "source",
	})
	require.Error(t, err)

	runner := &localRunner{request: LocalActivityInput{Width: 320, Height: 180, FPS: 24}}
	for _, rotation := range []float64{0, 90, -90, 45, 180} {
		filter := runner.visualScaleFilter(map[string]any{
			"w": 160, "h": 90, "fit": "contain", "bg": "#000000",
			"crop_x": 0.1, "crop_y": 0.1, "crop_width": 0.8, "crop_height": 0.8,
			"rotation_degrees": rotation,
		})
		assert.Contains(t, filter, "crop=")
		if rotation == 0 {
			assert.NotContains(t, filter, "rotate=")
			assert.NotContains(t, filter, "geq=")
		} else {
			assert.Contains(t, filter, "rotate=")
			assert.Contains(t, filter, "geq=")
		}
	}
	dipArgs := runner.argsTransitionAudio("from.m4a", "to.m4a", map[string]any{
		"kind": "dipToBlack", "duration": 1.0, "from_gain": 0.8, "to_gain": 0.6,
	}, "out.m4a")
	assert.Contains(t, strings.Join(dipArgs, " "), "afade=t=out")
	assert.Contains(t, strings.Join(dipArgs, " "), "afade=t=in")
}
