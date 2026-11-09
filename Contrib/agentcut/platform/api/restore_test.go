package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/entity"
	"agentcut/timeline"
)

func restoreRequest(
	t *testing.T,
	handler http.Handler,
	projectID string,
	mutationID string,
	baseRevision uint64,
	doc timeline.Doc,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"clientMutationId": mutationID, "baseRevision": baseRevision, "doc": doc,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost, "/v1/projects/"+projectID+"/timeline:restore", bytes.NewReader(body),
	))
	return recorder
}

func TestRestoreTimelineAPISuccessConflictAndPermission(t *testing.T) {
	server, temporalClient := newTestServer()
	projectID := createProject(t, server.Handler())
	state := temporalClient.timelines[entity.TimelineWorkflowID(projectID)]
	before := state.Doc.Clone()
	changed := before.Clone()
	changed.Metadata.Name = "changed"
	state.Doc = changed
	state.Revision = 1

	success := restoreRequest(t, server.Handler(), projectID, "restore-1", 1, before)
	require.Equal(t, http.StatusOK, success.Code, success.Body.String())
	assert.Equal(t, before.Metadata.Name, state.Doc.Metadata.Name)
	assert.Equal(t, uint64(2), state.Revision)

	conflict := restoreRequest(t, server.Handler(), projectID, "restore-2", 1, changed)
	assert.Equal(t, http.StatusConflict, conflict.Code)
	assert.Contains(t, conflict.Body.String(), entity.ErrTypeRevisionConflict)

	body, err := json.Marshal(map[string]any{
		"clientMutationId": "restore-other", "baseRevision": 2, "doc": before,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost, "/v1/projects/"+projectID+"/timeline:restore", bytes.NewReader(body),
	)
	request.Header.Set("X-AgentCut-User", "other-user")
	denied := httptest.NewRecorder()
	server.Handler().ServeHTTP(denied, request)
	assert.Equal(t, http.StatusNotFound, denied.Code)
}
