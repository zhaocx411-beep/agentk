package preparemedia

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/platform/blob"
	"agentcut/timeline"
)

type managedTestStore struct {
	inner   blob.Storage
	mu      sync.Mutex
	putKeys map[string]int
	gets    int
}

func (s *managedTestStore) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (string, error) {
	_, err := s.inner.Put(ctx, key, body, size, contentType)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.putKeys == nil {
		s.putKeys = map[string]int{}
	}
	s.putKeys[key]++
	s.mu.Unlock()
	return "https://blob.example/bucket/" + key, nil
}

func (s *managedTestStore) Get(ctx context.Context, key string) (*blob.Object, error) {
	s.mu.Lock()
	s.gets++
	s.mu.Unlock()
	return s.inner.Get(ctx, key)
}

func (s *managedTestStore) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, key)
}
func (*managedTestStore) Backend() string { return "blob" }

type testThumbnailer struct{}

func (testThumbnailer) GenerateVideoSprite(context.Context, string, float64) ([]byte, error) {
	return []byte("thumbnail"), nil
}
func (testThumbnailer) GenerateImageThumbnail(context.Context, string) ([]byte, error) {
	return []byte("image-thumbnail"), nil
}

type testWaveform struct{ calls int }

func (w *testWaveform) Extract(context.Context, string) ([]float32, error) {
	w.calls++
	return []float32{0.2, 0.7}, nil
}

type testAudioExtractor struct{ calls int }

func (e *testAudioExtractor) ExtractForASR(_ context.Context, _, outputPath string) error {
	e.calls++
	return os.WriteFile(outputPath, []byte("mp3"), 0o600)
}

func TestLocalArtifactsVideoAndDeterministicDerivatives(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	sourceKey := "agentcut/p1/source/m1/video.mp4"
	_, err = store.Put(context.Background(), sourceKey, bytes.NewReader([]byte("video")), 5, "video/mp4")
	require.NoError(t, err)
	waveform := &testWaveform{}
	audio := &testAudioExtractor{}
	activities := &Activities{
		Store: store, Thumbnailer: testThumbnailer{},
		WaveformExtractor: waveform, AudioExtractor: audio,
	}
	req := LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1", URI: "https://blob.example/bucket/" + sourceKey,
			BlobKey: sourceKey, Kind: timeline.MediaVideo,
			Probe: &timeline.MediaProbe{DurationMs: 3000, HasAudio: true},
		},
	}
	first, err := activities.LocalArtifacts(context.Background(), req)
	require.NoError(t, err)
	second, err := activities.LocalArtifacts(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, timeline.ProcessingSucceeded, first.ThumbnailStage.State)
	assert.Equal(t, timeline.ProcessingSucceeded, first.WaveformStage.State)
	assert.Equal(t, timeline.ProcessingPending, first.TranscriptStage.State)
	assert.Equal(t, blob.ThumbnailKey("p1", "m1"), first.ThumbnailKey)
	assert.Equal(t, "https://blob.example/bucket/"+blob.ASRAudioKey("p1", "m1"), first.ASRAudioURI)
	assert.Equal(t, first.ThumbnailKey, second.ThumbnailKey)
	assert.Equal(t, first.ASRAudioURI, second.ASRAudioURI)
	assert.Equal(t, 2, store.putKeys[blob.ThumbnailKey("p1", "m1")])
	assert.Equal(t, 2, store.putKeys[blob.ASRAudioKey("p1", "m1")])
	assert.Equal(t, 2, waveform.calls)
	assert.Equal(t, 2, audio.calls)
}

func TestLocalArtifactsNoAudioSkipsDependentStages(t *testing.T) {
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	sourceKey := "agentcut/p1/source/m1/video.mp4"
	_, err = store.Put(context.Background(), sourceKey, bytes.NewReader([]byte("video")), 5, "video/mp4")
	require.NoError(t, err)
	waveform := &testWaveform{}
	audio := &testAudioExtractor{}
	activities := &Activities{
		Store: store, Thumbnailer: testThumbnailer{},
		WaveformExtractor: waveform, AudioExtractor: audio,
	}
	result, err := activities.LocalArtifacts(context.Background(), LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1", URI: "/local", BlobKey: sourceKey, Kind: timeline.MediaVideo,
			Probe: &timeline.MediaProbe{DurationMs: 1000, HasAudio: false},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, timeline.ProcessingSkipped, result.WaveformStage.State)
	assert.Equal(t, timeline.ProcessingSkipped, result.TranscriptStage.State)
	assert.Zero(t, waveform.calls)
	assert.Zero(t, audio.calls)
}

func TestLocalArtifactsRejectsSSRFBeforeStorageRead(t *testing.T) {
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	activities := &Activities{Store: store}
	_, err = activities.LocalArtifacts(context.Background(), LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1", URI: "http://127.0.0.1/admin", Kind: timeline.MediaVideo,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNTRUSTED_MEDIA_SOURCE")
	assert.Zero(t, store.gets)
}

func TestLocalArtifactsRejectsCrossProjectSourceBeforeStorageRead(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	activities := &Activities{Store: store}
	_, err = activities.LocalArtifacts(context.Background(), LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1",
			URI:     "https://blob.example/bucket/agentcut/p2/source/m1/video.mp4",
			BlobKey: "agentcut/p2/source/m1/video.mp4",
			Kind:    timeline.MediaVideo,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNTRUSTED_MEDIA_SOURCE")
	assert.Zero(t, store.gets)
}

func TestLocalArtifactsTranscodesLargeAudioBeforeASR(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	sourceKey := "agentcut/p1/source/m1/large.wav"
	large := bytes.Repeat([]byte{1}, int(maxASRAudioBytes+1))
	_, err = store.Put(context.Background(), sourceKey, bytes.NewReader(large), int64(len(large)), "audio/wav")
	require.NoError(t, err)
	audio := &testAudioExtractor{}
	result, err := (&Activities{
		Store: store, WaveformExtractor: &testWaveform{}, AudioExtractor: audio,
	}).LocalArtifacts(context.Background(), LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1", Generation: 4,
			URI: "https://blob.example/bucket/" + sourceKey, BlobKey: sourceKey,
			Kind:  timeline.MediaAudio,
			Probe: &timeline.MediaProbe{DurationMs: 60_000, HasAudio: true},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, audio.calls)
	assert.Equal(t, timeline.ProcessingPending, result.TranscriptStage.State)
	assert.Equal(t, "https://blob.example/bucket/"+
		blob.ASRAudioKeyForGeneration("p1", "m1", 4), result.ASRAudioURI)
}

func TestLocalArtifactsPassesThroughSmallManagedAudio(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "https://blob.example")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "bucket")
	local, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	store := &managedTestStore{inner: local}
	sourceKey := "agentcut/p1/source/m1/small.mp3"
	_, err = store.Put(context.Background(), sourceKey, bytes.NewReader([]byte("small")), 5, "audio/mpeg")
	require.NoError(t, err)
	audio := &testAudioExtractor{}
	result, err := (&Activities{
		Store: store, WaveformExtractor: &testWaveform{}, AudioExtractor: audio,
	}).LocalArtifacts(context.Background(), LocalArtifactsRequest{
		ProjectID: "p1", FPS: 30,
		Media: timeline.Media{
			MediaID: "m1", URI: "https://blob.example/bucket/" + sourceKey,
			BlobKey: sourceKey, Kind: timeline.MediaAudio,
			Probe: &timeline.MediaProbe{DurationMs: 1000, HasAudio: true},
		},
	})
	require.NoError(t, err)
	assert.Zero(t, audio.calls)
	assert.Equal(t, "https://blob.example/bucket/"+sourceKey, result.ASRAudioURI)
}

type encodedValue struct{ value any }

func (v encodedValue) HasValue() bool { return true }
func (v encodedValue) Get(out any) error {
	raw, _ := json.Marshal(v.value)
	return json.Unmarshal(raw, out)
}

type updateHandle struct{ err error }

func (*updateHandle) WorkflowID() string               { return "" }
func (*updateHandle) RunID() string                    { return "" }
func (*updateHandle) UpdateID() string                 { return "" }
func (h *updateHandle) Get(context.Context, any) error { return h.err }

type conflictTemporal struct {
	queries int
	updates []client.UpdateWorkflowOptions
	always  bool
}

func (f *conflictTemporal) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	f.queries++
	return encodedValue{value: entity.TimelineState{ProjectID: "p1", Revision: uint64(f.queries)}}, nil
}

func (f *conflictTemporal) UpdateWorkflow(_ context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error) {
	f.updates = append(f.updates, options)
	if f.always || len(f.updates) == 1 {
		return &updateHandle{err: temporal.NewApplicationError("conflict", entity.ErrTypeRevisionConflict)}, nil
	}
	return &updateHandle{}, nil
}

func TestUpdateMediaStopsAfterFiniteConflictRetries(t *testing.T) {
	fake := &conflictTemporal{always: true}
	activities := &Activities{Temporal: fake}
	processing := InitialProcessing(timeline.Media{
		MediaID: "m1", Kind: timeline.MediaVideo,
		Probe: &timeline.MediaProbe{DurationMs: 1000},
	})
	err := activities.UpdateMedia(context.Background(), UpdateMediaRequest{
		ProjectID: "p1", MediaID: "m1", Stage: "probe",
		Patch: timeline.MediaPatch{Processing: &processing},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revision conflicts")
	assert.Len(t, fake.updates, 4)
}

func TestUpdateMediaRetriesRevisionConflictWithStableAttemptIDs(t *testing.T) {
	fake := &conflictTemporal{}
	activities := &Activities{Temporal: fake}
	processing := InitialProcessing(timeline.Media{
		MediaID: "m1", Kind: timeline.MediaVideo,
		Probe: &timeline.MediaProbe{DurationMs: 1000},
	})
	err := activities.UpdateMedia(context.Background(), UpdateMediaRequest{
		ProjectID: "p1", MediaID: "m1", Stage: "probe",
		Patch: timeline.MediaPatch{Processing: &processing},
	})
	require.NoError(t, err)
	require.Len(t, fake.updates, 2)
	assert.Equal(t, "prepare:p1:m1:probe:0", fake.updates[0].UpdateID)
	assert.Equal(t, "prepare:p1:m1:probe:1", fake.updates[1].UpdateID)
	first := fake.updates[0].Args[0].(entity.ApplyOpsRequest)
	second := fake.updates[1].Args[0].(entity.ApplyOpsRequest)
	assert.Equal(t, uint64(1), first.BaseRevision)
	assert.Equal(t, uint64(2), second.BaseRevision)
}
