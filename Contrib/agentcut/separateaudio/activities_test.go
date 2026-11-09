package separateaudio

import (
	"context"
	"encoding/json"
	"io"
	"os"
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

type encodedState struct{ state entity.TimelineState }

func (e encodedState) HasValue() bool { return true }
func (e encodedState) Get(value any) error {
	raw, _ := json.Marshal(e.state)
	return json.Unmarshal(raw, value)
}

type updateHandle struct {
	result any
	err    error
}

func (*updateHandle) WorkflowID() string { return "" }
func (*updateHandle) RunID() string      { return "" }
func (*updateHandle) UpdateID() string   { return "" }
func (h *updateHandle) Get(_ context.Context, value any) error {
	if h.err != nil {
		return h.err
	}
	raw, _ := json.Marshal(h.result)
	return json.Unmarshal(raw, value)
}

type activityTemporal struct {
	state entity.TimelineState
}

func (f *activityTemporal) QueryWorkflow(
	context.Context, string, string, string, ...any,
) (converter.EncodedValue, error) {
	return encodedState{state: f.state}, nil
}

func (f *activityTemporal) UpdateWorkflow(
	_ context.Context, options client.UpdateWorkflowOptions,
) (client.WorkflowUpdateHandle, error) {
	req := options.Args[0].(entity.ApplyOpsRequest)
	if req.BaseRevision != f.state.Revision {
		return &updateHandle{err: temporal.NewNonRetryableApplicationError(
			"conflict", entity.ErrTypeRevisionConflict, nil,
		)}, nil
	}
	ops := append([]timeline.Operation(nil), req.Operations...)
	for index := range ops {
		if ops[index].Op == timeline.OpAddMedia && ops[index].Media != nil {
			media := *ops[index].Media
			media.Generation = 1
			ops[index].Media = &media
		}
	}
	next, summary, err := timeline.ApplyOps(f.state.Doc, ops)
	if err != nil {
		return &updateHandle{err: err}, nil
	}
	f.state.Doc = next
	f.state.Revision++
	return &updateHandle{result: entity.ApplyOpsResult{
		Revision: f.state.Revision, Summary: summary,
	}}, nil
}

type fakeEditingExtractor struct {
	err error
}

func (f fakeEditingExtractor) ExtractForEditing(
	_ context.Context, _ string, output string,
) error {
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(output, []byte("standalone-audio"), 0o600)
}

type closingStore struct{ blob.Storage }

func (s closingStore) Put(
	ctx context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) (string, error) {
	uri, err := s.Storage.Put(ctx, key, body, size, contentType)
	if closer, ok := body.(io.Closer); ok {
		_ = closer.Close()
	}
	return uri, err
}

func separationFixture(t *testing.T) (*Activities, *activityTemporal, Input) {
	t.Helper()
	doc := timeline.NewDoc("separate", 1920, 1080, 30)
	doc.Media = []timeline.Media{{
		MediaID: "video", Generation: 2, URI: "/video", BlobKey: blob.SourceKey("p1", "video", "source.mp4"),
		Kind: timeline.MediaVideo, DurationInFrames: 300,
		Probe:         &timeline.MediaProbe{DurationMs: 10000, HasAudio: true, Codec: "h264"},
		WaveformPeaks: []float32{0.1, 0.8, 0.3},
	}}
	doc.Tracks = []timeline.Track{
		{TrackID: "v1", Kind: timeline.TrackVideo},
		{TrackID: "a1", Kind: timeline.TrackAudio},
	}
	doc.Items = []timeline.Item{{
		ItemID: "clip", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "video",
		From: 30, DurationInFrames: 90, TrimStart: 15, Speed: 1.25,
		Volume: 0.8, FadeInFrames: 3, FadeOutFrames: 4,
	}}
	temporalClient := &activityTemporal{state: entity.TimelineState{
		ProjectID: "p1", Revision: 7, Doc: doc,
	}}
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	source, err := os.CreateTemp(t.TempDir(), "video-*.mp4")
	require.NoError(t, err)
	_, err = source.Write([]byte("video-source"))
	require.NoError(t, err)
	require.NoError(t, source.Close())
	file, err := os.Open(source.Name())
	require.NoError(t, err)
	_, err = store.Put(context.Background(), doc.Media[0].BlobKey, file, 12, "video/mp4")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	return &Activities{
			Temporal: temporalClient, Store: closingStore{store}, Extractor: fakeEditingExtractor{},
		}, temporalClient, Input{
			ProjectID: "p1", ItemID: "clip", BaseRevision: 7, ClientMutation: "request-1",
		}
}

func TestExtractThenCommitSeparatedAudioAtomically(t *testing.T) {
	activities, temporalClient, input := separationFixture(t)
	extracted, err := activities.Extract(context.Background(), ExtractRequest{Input: input})
	require.NoError(t, err)
	assert.Equal(t, timeline.MediaAudio, extracted.AudioMedia.Kind)
	assert.NotEqual(t, "video", extracted.AudioMedia.MediaID)
	assert.Equal(t, []float32{0.1, 0.8, 0.3}, extracted.AudioMedia.WaveformPeaks)
	assert.Equal(t, timeline.ProcessingSucceeded, extracted.AudioMedia.Processing.Waveform)

	output, err := activities.Commit(context.Background(), CommitRequest{
		Input: input, Extract: extracted,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(8), output.Revision)
	audioItem := temporalClient.state.Doc.Items[len(temporalClient.state.Doc.Items)-1]
	assert.Equal(t, timeline.ItemAudio, audioItem.Kind)
	assert.Equal(t, int64(30), audioItem.From)
	assert.Equal(t, int64(15), audioItem.TrimStart)
	assert.Equal(t, 1.25, audioItem.Speed)
	assert.True(t, temporalClient.state.Doc.Items[0].Muted)
	assert.Equal(t, "a1", audioItem.TrackID)
	assert.Equal(t, []float32{0.1, 0.8, 0.3}, temporalClient.state.Doc.Media[1].WaveformPeaks)
}

func TestExtractRejectsNoAudioAndRevisionConflictBeforeFFmpeg(t *testing.T) {
	activities, temporalClient, input := separationFixture(t)
	temporalClient.state.Doc.Media[0].Probe.HasAudio = false
	_, err := activities.Extract(context.Background(), ExtractRequest{Input: input})
	require.Error(t, err)
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, "NO_AUDIO", appErr.Type())

	temporalClient.state.Doc.Media[0].Probe.HasAudio = true
	input.BaseRevision = 6
	_, err = activities.Extract(context.Background(), ExtractRequest{Input: input})
	require.Error(t, err)
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, entity.ErrTypeRevisionConflict, appErr.Type())
}

func TestCommitIsIdempotentAfterSuccess(t *testing.T) {
	activities, _, input := separationFixture(t)
	extracted, err := activities.Extract(context.Background(), ExtractRequest{Input: input})
	require.NoError(t, err)
	first, err := activities.Commit(context.Background(), CommitRequest{Input: input, Extract: extracted})
	require.NoError(t, err)
	second, err := activities.Commit(context.Background(), CommitRequest{Input: input, Extract: extracted})
	require.NoError(t, err)
	assert.Equal(t, first.Revision, second.Revision)
	assert.True(t, second.Reused)
}
