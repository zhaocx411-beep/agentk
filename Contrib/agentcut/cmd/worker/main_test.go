package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentcut/platform/agentkclient"
	"agentcut/tools"
)

type fakeAgentKAuthenticator struct {
	identity agentkclient.Identity
	err      error
}

func (f fakeAgentKAuthenticator) ValidateAuth(context.Context) (agentkclient.Identity, error) {
	return f.identity, f.err
}

func TestValidateAgentKAuth(t *testing.T) {
	expected := agentkclient.Identity{AppID: "app-agentcut", DisplayName: "AgentCut"}
	actual, err := validateAgentKAuth(context.Background(), fakeAgentKAuthenticator{identity: expected})
	require.NoError(t, err)
	assert.Equal(t, expected, actual)

	_, err = validateAgentKAuth(context.Background(), fakeAgentKAuthenticator{
		err: errors.New("unauthenticated"),
	})
	require.ErrorContains(t, err, "unauthenticated")

	_, err = validateAgentKAuth(context.Background(), fakeAgentKAuthenticator{
		identity: agentkclient.Identity{DisplayName: "AgentCut"},
	})
	require.ErrorContains(t, err, "empty app id")
}

func TestValidateWorkerLaneEnvironment(t *testing.T) {
	t.Run("managed baseline", func(t *testing.T) {
		t.Setenv("KWS_SERVICE_STAGE", "STAGING")
		t.Setenv("KWS_LANE_ID", "")
		lane, err := validateWorkerLaneEnvironment()
		require.NoError(t, err)
		assert.Empty(t, lane)
	})

	t.Run("named lane", func(t *testing.T) {
		t.Setenv("KWS_SERVICE_STAGE", "STAGING")
		t.Setenv("KWS_LANE_ID", "dev_agentcut")
		lane, err := validateWorkerLaneEnvironment()
		require.NoError(t, err)
		assert.Equal(t, "dev_agentcut", lane)
	})

	t.Run("rejects accidental local lane", func(t *testing.T) {
		t.Setenv("KWS_SERVICE_STAGE", "")
		t.Setenv("KWS_LANE_ID", "")
		_, err := validateWorkerLaneEnvironment()
		require.ErrorContains(t, err, "KWS_SERVICE_STAGE is required")
	})

	t.Run("rejects invalid named lane", func(t *testing.T) {
		t.Setenv("KWS_SERVICE_STAGE", "STAGING")
		t.Setenv("KWS_LANE_ID", "invalid lane")
		_, err := validateWorkerLaneEnvironment()
		require.ErrorContains(t, err, "KWS_LANE_ID")
	})
}

func TestInternalWorkerConfig(t *testing.T) {
	getenv := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}

	t.Run("baseline defaults to internal worker", func(t *testing.T) {
		run, queue, err := internalWorkerConfig("", getenv(nil))
		require.NoError(t, err)
		assert.True(t, run)
		assert.Equal(t, tools.InternalTaskQueue, queue)
	})

	t.Run("named lane skips internal worker by default", func(t *testing.T) {
		run, queue, err := internalWorkerConfig("dev_alice", getenv(nil))
		require.NoError(t, err)
		assert.False(t, run)
		assert.Equal(t, tools.InternalTaskQueue, queue)
	})

	t.Run("named lane can use a private internal queue", func(t *testing.T) {
		run, queue, err := internalWorkerConfig("dev_alice", getenv(map[string]string{
			"AGENTCUT_RUN_INTERNAL_WORKER": "true",
			"AGENTCUT_TASK_QUEUE":          "contrib.agentcut.internal.dev_alice",
		}))
		require.NoError(t, err)
		assert.True(t, run)
		assert.Equal(t, "contrib.agentcut.internal.dev_alice", queue)
	})

	t.Run("named lane requires an explicit private queue", func(t *testing.T) {
		_, _, err := internalWorkerConfig("dev_alice", getenv(map[string]string{
			"AGENTCUT_RUN_INTERNAL_WORKER": "true",
		}))
		require.ErrorContains(t, err, "explicit private AGENTCUT_TASK_QUEUE")

		for _, queue := range []string{tools.InternalTaskQueue, tools.WorkerTaskQueue} {
			_, _, err = internalWorkerConfig("dev_alice", getenv(map[string]string{
				"AGENTCUT_RUN_INTERNAL_WORKER": "true",
				"AGENTCUT_TASK_QUEUE":          queue,
			}))
			require.ErrorContains(t, err, "must not use shared")
		}
	})

	t.Run("explicit false disables baseline internal worker", func(t *testing.T) {
		run, _, err := internalWorkerConfig("", getenv(map[string]string{
			"AGENTCUT_RUN_INTERNAL_WORKER": "false",
		}))
		require.NoError(t, err)
		assert.False(t, run)
	})

	t.Run("invalid boolean is rejected", func(t *testing.T) {
		_, _, err := internalWorkerConfig("", getenv(map[string]string{
			"AGENTCUT_RUN_INTERNAL_WORKER": "yes",
		}))
		require.ErrorContains(t, err, "must be true or false")
	})
}
