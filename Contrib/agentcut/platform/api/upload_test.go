package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/entity"
	"agentcut/platform/blob"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

type fakeProber struct {
	result timeline.MediaProbe
	err    error
}

func (p fakeProber) Probe(context.Context, string) (timeline.MediaProbe, error) {
	return p.result, p.err
}

type fakeAudioPreviewer struct {
	output []byte
	calls  int
}

func (p *fakeAudioPreviewer) TranscodeForPreview(
	_ context.Context,
	_ string,
	outputPath string,
) error {
	p.calls++
	return os.WriteFile(outputPath, p.output, 0o600)
}

func uploadRequest(t *testing.T, path, kind, duration string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "sample.mp4")
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.WriteField("kind", kind))
	if duration != "" {
		require.NoError(t, mw.WriteField("durationMs", duration))
	}
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestUploadUsesServerProbeDuration(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	s.Prober = fakeProber{result: timeline.MediaProbe{
		DurationMs: 2500, Width: 1280, Height: 720, NativeFPS: 25, HasAudio: true, Codec: "h264",
	}}
	projectID := createProject(t, s.Handler())

	rec := httptest.NewRecorder()
	mediaBytes := bytes.Repeat([]byte("m"), 4096)
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "video", "9999", mediaBytes))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var response struct {
		Media timeline.Media `json:"media"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, int64(75), response.Media.DurationInFrames)
	assert.Equal(t, "/v1/projects/"+projectID+"/media/"+response.Media.MediaID+"/content", response.Media.URI)
	require.NotNil(t, response.Media.Probe)
	assert.Equal(t, int64(2500), response.Media.Probe.DurationMs)
	assert.Equal(t, 1280, response.Media.Probe.Width)

	content := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+projectID+"/media/"+response.Media.MediaID+"/content", nil)
	s.Handler().ServeHTTP(content, req)
	require.Equal(t, http.StatusOK, content.Code, content.Body.String())
	assert.Equal(t, mediaBytes, content.Body.Bytes())
	assert.Contains(t, content.Header().Get("Content-Disposition"), "inline")

	missing := httptest.NewRecorder()
	s.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet,
		"/v1/projects/"+projectID+"/media/nope/content", nil))
	assert.Equal(t, http.StatusNotFound, missing.Code)
}

func TestMediaThumbnailServesGenerationScopedDerivedKey(t *testing.T) {
	s, ft := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	projectID := createProject(t, s.Handler())
	mediaID := "med_thumbnail"
	key := blob.ThumbnailKeyForGeneration(projectID, mediaID, 1)
	thumbnail := []byte("thumbnail-bytes")
	_, err = store.Put(
		context.Background(), key, bytes.NewReader(thumbnail),
		int64(len(thumbnail)), "image/jpeg",
	)
	require.NoError(t, err)

	workflowID := entity.TimelineWorkflowID(projectID)
	state := ft.timelines[workflowID]
	state.Doc.Media = append(state.Doc.Media, timeline.Media{
		MediaID: mediaID, Generation: 1, Kind: timeline.MediaVideo,
		ThumbnailKey: key,
	})
	ft.timelines[workflowID] = state

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet,
		"/v1/projects/"+projectID+"/media/"+mediaID+"/thumbnail",
		nil,
	))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, thumbnail, rec.Body.Bytes())
	assert.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
}

func TestAudioPlaybackTranscodesAndCachesBrowserCompatibleMP3(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	previewer := &fakeAudioPreviewer{output: []byte("browser-mp3")}
	s.Blob = store
	s.AudioPreviewer = previewer
	s.Prober = fakeProber{result: timeline.MediaProbe{
		DurationMs: 2500, HasAudio: true, Codec: "pcm_f64le",
	}}
	projectID := createProject(t, s.Handler())

	upload := httptest.NewRecorder()
	s.Handler().ServeHTTP(upload,
		uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "audio", "", []byte("wav-source")))
	require.Equal(t, http.StatusCreated, upload.Code, upload.Body.String())
	var response struct {
		Media timeline.Media `json:"media"`
	}
	require.NoError(t, json.Unmarshal(upload.Body.Bytes(), &response))
	path := "/v1/projects/" + projectID + "/media/" + response.Media.MediaID + "/playback"

	first := httptest.NewRecorder()
	s.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.Equal(t, "audio/mpeg", first.Header().Get("Content-Type"))
	assert.Equal(t, "browser-mp3", first.Body.String())

	rangedRequest := httptest.NewRequest(http.MethodGet, path, nil)
	rangedRequest.Header.Set("Range", "bytes=0-2")
	ranged := httptest.NewRecorder()
	s.Handler().ServeHTTP(ranged, rangedRequest)
	require.Equal(t, http.StatusPartialContent, ranged.Code, ranged.Body.String())
	assert.Equal(t, "bro", ranged.Body.String())
	assert.Equal(t, 1, previewer.calls, "cached preview should not transcode again")
}

func TestUploadReturnsBeforeAsyncArtifacts(t *testing.T) {
	s, ft := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	s.Prober = fakeProber{result: timeline.MediaProbe{
		DurationMs: 4000, Width: 640, Height: 360, HasAudio: true, Codec: "h264",
	}}
	projectID := createProject(t, s.Handler())

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "video", "", []byte("video-bytes")))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var response struct {
		Media timeline.Media `json:"media"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Empty(t, response.Media.WaveformPeaks)
	assert.Empty(t, response.Media.ThumbnailURI)
	assert.Empty(t, response.Media.ThumbnailKey)
	require.NotNil(t, response.Media.Processing)
	assert.True(t, response.Media.Processing.ReadyForTimeline)
	assert.Equal(t, "processing", response.Media.Processing.Status)
	assert.Equal(t, timeline.ProcessingSucceeded, response.Media.Processing.Probe)
	assert.Equal(t, timeline.ProcessingPending, response.Media.Processing.Thumbnail)
	assert.Equal(t, timeline.ProcessingPending, response.Media.Processing.Waveform)
	assert.Equal(t, timeline.ProcessingPending, response.Media.Processing.Transcript)
	assert.Equal(t, timeline.ProcessingPending, response.Media.Processing.Understanding)
	assert.Equal(t, 1, ft.prepares[preparemedia.WorkflowID(
		projectID, response.Media.MediaID, response.Media.Generation)])

	retry := httptest.NewRecorder()
	s.Handler().ServeHTTP(retry, httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+projectID+"/media/"+response.Media.MediaID+"/prepare", nil))
	require.Equal(t, http.StatusAccepted, retry.Code, retry.Body.String())
	assert.Equal(t, 2, ft.prepares[preparemedia.WorkflowID(
		projectID, response.Media.MediaID, response.Media.Generation)])

	missing := httptest.NewRecorder()
	s.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodPost,
		"/v1/projects/"+projectID+"/media/missing/prepare", nil))
	assert.Equal(t, http.StatusNotFound, missing.Code)
}

func TestUploadNoAudioInitialStates(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	s.Prober = fakeProber{result: timeline.MediaProbe{DurationMs: 4000, HasAudio: false}}
	projectID := createProject(t, s.Handler())

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "video", "", []byte("video-bytes")))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var response struct {
		Media timeline.Media `json:"media"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Empty(t, response.Media.ThumbnailKey)
	assert.Empty(t, response.Media.ThumbnailURI)
	assert.Empty(t, response.Media.WaveformPeaks)
	require.NotNil(t, response.Media.Processing)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Waveform)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Transcript)
}

func TestImageUploadProcessingCompatibility(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	projectID := createProject(t, s.Handler())

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "image", "", []byte("image-bytes")))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var response struct {
		Media timeline.Media `json:"media"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotNil(t, response.Media.Processing)
	assert.True(t, response.Media.Processing.ReadyForTimeline)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Probe)
	assert.Equal(t, timeline.ProcessingPending, response.Media.Processing.Thumbnail)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Waveform)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Transcript)
	assert.Equal(t, timeline.ProcessingSkipped, response.Media.Processing.Understanding)
}

func TestUploadProbeFailureIsActionable(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	s.Prober = fakeProber{err: assert.AnError}
	projectID := createProject(t, s.Handler())

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "audio", "", []byte("bad")))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "MEDIA_PROBE_FAILED")
	assert.Contains(t, rec.Body.String(), "ffprobe")
}

func TestUploadWithoutBlobListsMissingConfiguration(t *testing.T) {
	t.Setenv("AGENTCUT_BLOB_ENDPOINT", "")
	t.Setenv("AGENTCUT_BLOB_BUCKET", "")
	s, _ := newTestServer()
	projectID := createProject(t, s.Handler())
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t, "/v1/projects/"+projectID+"/uploads", "image", "", []byte("image")))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "BLOB_NOT_CONFIGURED")
	assert.Contains(t, rec.Body.String(), "AGENTCUT_BLOB_ENDPOINT")
	assert.Contains(t, rec.Body.String(), "AGENTCUT_BLOB_BUCKET")
}

func TestUploadCompensationKeepsBlobWhenUncertainUpdateCommitted(t *testing.T) {
	s, ft := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	s.Prober = fakeProber{result: timeline.MediaProbe{DurationMs: 1000, HasAudio: false}}
	projectID := createProject(t, s.Handler())
	ft.failApplyAfterCommit = true

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, uploadRequest(t,
		"/v1/projects/"+projectID+"/uploads", "audio", "", []byte("audio-bytes")))
	require.Equal(t, http.StatusBadGateway, rec.Code)

	state := ft.timelines[entity.TimelineWorkflowID(projectID)]
	require.Len(t, state.Doc.Media, 1, "fake simulates a committed update with lost response")
	object, err := store.Get(context.Background(), state.Doc.Media[0].BlobKey)
	require.NoError(t, err, "compensation must not delete a referenced upload")
	require.NoError(t, object.Body.Close())
}

func TestMediaHandlersRejectUnsafePathMediaID(t *testing.T) {
	s, _ := newTestServer()
	store, err := blob.NewLocalStore(t.TempDir())
	require.NoError(t, err)
	s.Blob = store
	projectID := createProject(t, s.Handler())

	for _, handler := range []http.HandlerFunc{
		s.handleMediaContent, s.handleMediaPlayback, s.handleMediaThumbnail, s.handlePrepareMedia,
	} {
		req := httptest.NewRequest(http.MethodGet, "/unsafe", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey{}, "dev"))
		req.SetPathValue("projectID", projectID)
		req.SetPathValue("mediaID", "../escape")
		rec := httptest.NewRecorder()
		handler(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	}
}
