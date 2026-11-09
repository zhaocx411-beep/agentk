package api

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	e2eprofile "agentcut/agents/e2e"
)

func TestBuildSessionProfileInjectsTranscriptProjectDefault(t *testing.T) {
	template, err := os.ReadFile("../../agents/agentcut/profile.json")
	require.NoError(t, err)
	profile, err := BuildSessionProfile(template, "project-123")
	require.NoError(t, err)
	defaults := profile.ToolDefaults["timeline_edit_transcript"]
	require.NotNil(t, defaults)
	require.Contains(t, defaults.Fields, "projectId")
	assert.Equal(t, "project-123", defaults.Fields["projectId"].GetStringValue())
}

func TestBuildE2ESessionProfileOnlyDefaultsTimelineGet(t *testing.T) {
	profile, err := BuildSessionProfile(e2eprofile.ProfileJSON, "project-e2e")
	require.NoError(t, err)
	require.Len(t, profile.Tools, 1)
	assert.Equal(t, "timeline_get", profile.Tools[0].GetName())
	assert.Equal(
		t,
		"project-e2e",
		profile.ToolDefaults["timeline_get"].Fields["projectId"].GetStringValue(),
	)
}
