package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/requestlane"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/platform/agentkclient"
)

type fakeAgentKAuthenticator struct {
	identity agentkclient.Identity
	err      error
	called   bool
}

func (f *fakeAgentKAuthenticator) ValidateAuth(context.Context) (agentkclient.Identity, error) {
	f.called = true
	return f.identity, f.err
}

func TestPreflightAgentKAllowsManualOnly(t *testing.T) {
	identity, configured, err := preflightAgentK(context.Background(), nil)
	require.NoError(t, err)
	assert.False(t, configured)
	assert.Equal(t, agentkclient.Identity{}, identity)
}

func TestPreflightAgentKValidatesConfiguredEndpoint(t *testing.T) {
	authenticator := &fakeAgentKAuthenticator{
		identity: agentkclient.Identity{
			AppID:       "app-agentcut",
			DisplayName: "AgentCut",
		},
	}
	identity, configured, err := preflightAgentK(context.Background(), authenticator)
	require.NoError(t, err)
	assert.True(t, configured)
	assert.True(t, authenticator.called)
	assert.Equal(t, authenticator.identity, identity)

	authenticator = &fakeAgentKAuthenticator{err: errors.New("unauthenticated")}
	_, configured, err = preflightAgentK(context.Background(), authenticator)
	assert.True(t, configured)
	require.ErrorContains(t, err, "unauthenticated")

	authenticator = &fakeAgentKAuthenticator{
		identity: agentkclient.Identity{DisplayName: "missing app"},
	}
	_, configured, err = preflightAgentK(context.Background(), authenticator)
	assert.True(t, configured)
	require.ErrorContains(t, err, "empty app id")
}

func TestRequestLaneHandlerExtractsKSIngressLane(t *testing.T) {
	t.Setenv("FALLBACK_TO_ENV_LANE_ID", "")
	t.Setenv("KWS_LANE_ID", "")
	var got requestlane.Lane
	handler, err := requestLaneHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			got, _ = requestlane.FromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		},
	))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("trace-context", `{"laneId":"dev_agentcut"}`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, requestlane.Lane("dev_agentcut"), got)
}

func TestProfileDefinitionsFromEnv(t *testing.T) {
	t.Setenv("AGENTCUT_E2E_PROFILE", "")
	profiles, err := profileDefinitionsFromEnv()
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.Equal(t, "agentcut", profiles[0].ProfileID)

	t.Setenv("AGENTCUT_E2E_PROFILE", "true")
	profiles, err = profileDefinitionsFromEnv()
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	assert.Equal(t, "agentcut-e2e", profiles[1].ProfileID)
	assert.False(t, profiles[1].IsDefault)

	t.Setenv("AGENTCUT_E2E_PROFILE", "yes")
	_, err = profileDefinitionsFromEnv()
	require.ErrorContains(t, err, "must be true or false")
}
