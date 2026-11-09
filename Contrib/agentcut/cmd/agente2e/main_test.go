package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRunnerConfigRequiresPersonalIsolation(t *testing.T) {
	values := map[string]string{
		"AGENTCUT_E2E_ALLOW_STAGING_WRITES": "true",
		"KWS_LANE_ID":                       "dev_alice",
		"AGENTCUT_DEV_USER":                 "alice",
		"AGENTCUT_TASK_QUEUE":               "contrib.agentcut.internal.dev_alice",
	}
	getenv := func(name string) string { return values[name] }
	config, err := loadRunnerConfig(getenv, "http://127.0.0.1:8080/", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "dev_alice", config.Lane)
	assert.Equal(t, "alice", config.User)
	assert.Equal(t, "http://127.0.0.1:8080", config.BaseURL)

	for name, value := range map[string]string{
		"AGENTCUT_E2E_ALLOW_STAGING_WRITES": "false",
		"KWS_LANE_ID":                       "",
		"AGENTCUT_DEV_USER":                 "dev",
		"AGENTCUT_TASK_QUEUE":               "contrib.agentcut",
	} {
		original := values[name]
		values[name] = value
		_, err := loadRunnerConfig(getenv, "http://127.0.0.1:8080", time.Minute)
		require.Error(t, err, name)
		values[name] = original
	}
}

func TestValidateEventsAcceptsOneTimelineGet(t *testing.T) {
	events := []sessionEvent{
		{
			ExecutionID: "exe-1",
			Kind:        "assistant.tool_call",
			Data: map[string]any{
				"toolCallId": "call-1",
				"toolName":   "timeline_get",
			},
		},
		{
			ExecutionID: "exe-1",
			Kind:        "tool.exec_succeeded",
			Data:        map[string]any{"toolCallId": "call-1"},
		},
		{
			ExecutionID: "exe-1",
			Kind:        "execution.finished",
			Data:        map[string]any{"status": "EXECUTION_STATUS_COMPLETED"},
		},
	}
	require.NoError(t, validateEvents(events, "exe-1"))

	events[0].Data["toolName"] = "agentk.generate_image"
	require.ErrorContains(t, validateEvents(events, "exe-1"), "only timeline_get")
}

func TestValidateEventsRejectsMissingSuccessAndFailedExecution(t *testing.T) {
	events := []sessionEvent{
		{
			ExecutionID: "exe-1",
			Kind:        "assistant.tool_call",
			Data: map[string]any{
				"toolCallId": "call-1",
				"toolName":   "timeline_get",
			},
		},
		{
			ExecutionID: "exe-1",
			Kind:        "execution.finished",
			Data: map[string]any{
				"status": "EXECUTION_STATUS_FAILED",
				"error":  "model unavailable",
			},
		},
	}
	require.ErrorContains(t, validateEvents(events, "exe-1"), "no matching success")
}
