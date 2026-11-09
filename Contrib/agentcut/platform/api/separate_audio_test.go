package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"

	"agentcut/entity"
	"agentcut/platform/blob"
	"agentcut/separateaudio"
	"agentcut/timeline"
)

func addSeparationFixture(t *testing.T, server *Server, temporalClient *fakeTemporal, projectID string) {
	t.Helper()
	state := temporalClient.timelines[entity.TimelineWorkflowID(projectID)]
	state.Revision = 4
	state.Doc.Media = []timeline.Media{{
		MediaID: "video", URI: "/video", BlobKey: blob.SourceKey(projectID, "video", "source.mp4"),
		Kind: timeline.MediaVideo, DurationInFrames: 120,
		Probe: &timeline.MediaProbe{DurationMs: 4000, HasAudio: true},
	}}
	state.Doc.Tracks = []timeline.Track{{TrackID: "v1", Kind: timeline.TrackVideo}}
	state.Doc.Items = []timeline.Item{{
		ItemID: "clip", TrackID: "v1", Kind: timeline.ItemVideo, MediaID: "video",
		From: 0, DurationInFrames: 60,
	}}
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	server.Blob = store
}

func separateRequest(t *testing.T, handler http.Handler, projectID string, revision uint64) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"clientMutationId": "separate-1", "baseRevision": revision,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost,
		"/v1/projects/"+projectID+"/items/clip/audio-separation",
		bytes.NewReader(body),
	))
	return recorder
}

func TestSeparateAudioAPIStartsPollsAndIsIdempotent(t *testing.T) {
	server, temporalClient := newTestServer()
	projectID := createProject(t, server.Handler())
	addSeparationFixture(t, server, temporalClient, projectID)

	first := separateRequest(t, server.Handler(), projectID, 4)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	workflowID := separateaudio.WorkflowID(projectID, "clip")
	assert.Equal(t, 1, temporalClient.separations[workflowID].starts)

	repeated := separateRequest(t, server.Handler(), projectID, 4)
	require.Equal(t, http.StatusAccepted, repeated.Code, repeated.Body.String())
	assert.Equal(t, 1, temporalClient.separations[workflowID].starts)

	outputDoc := temporalClient.timelines[entity.TimelineWorkflowID(projectID)].Doc.Clone()
	temporalClient.separations[workflowID].status = enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED
	temporalClient.separations[workflowID].output = separateaudio.Output{
		Revision: 5, Doc: outputDoc, AudioMediaID: "audio", AudioItemID: "audio-item",
		AudioTrackID: "a1",
	}
	status := httptest.NewRecorder()
	server.Handler().ServeHTTP(status, httptest.NewRequest(
		http.MethodGet,
		"/v1/projects/"+projectID+"/items/clip/audio-separation",
		nil,
	))
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	assert.Contains(t, status.Body.String(), `"status":"completed"`)
	assert.Contains(t, status.Body.String(), `"audioMediaId":"audio"`)
}

func TestSeparateAudioAPIRejectsConflictNoAudioAndLockedTrack(t *testing.T) {
	server, temporalClient := newTestServer()
	projectID := createProject(t, server.Handler())
	addSeparationFixture(t, server, temporalClient, projectID)

	conflict := separateRequest(t, server.Handler(), projectID, 3)
	assert.Equal(t, http.StatusConflict, conflict.Code)

	state := temporalClient.timelines[entity.TimelineWorkflowID(projectID)]
	state.Doc.Media[0].Probe.HasAudio = false
	noAudio := separateRequest(t, server.Handler(), projectID, 4)
	assert.Equal(t, http.StatusUnprocessableEntity, noAudio.Code)
	assert.Contains(t, noAudio.Body.String(), "NO_AUDIO")

	state.Doc.Media[0].Probe.HasAudio = true
	state.Doc.Tracks[0].Locked = true
	locked := separateRequest(t, server.Handler(), projectID, 4)
	assert.Equal(t, http.StatusUnprocessableEntity, locked.Code)
	assert.Contains(t, locked.Body.String(), "LOCKED_TRACK")
}
