package tools

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscriptCapabilityAndProfileContract(t *testing.T) {
	var found bool
	for _, definition := range Definitions() {
		if definition.Capability.ID != CapTimelineEditTranscript {
			continue
		}
		found = true
		schema, err := json.Marshal(definition.Capability.InputSchema)
		require.NoError(t, err)
		assert.Contains(t, string(schema), "occurrence")
		assert.NotContains(t, string(schema), "fromFrame")
		assert.NotContains(t, string(schema), "toFrame")
	}
	require.True(t, found, "transcript capability must be registered")

	profile, err := os.ReadFile("../agents/agentcut/profile.json")
	require.NoError(t, err)
	assert.Contains(t, string(profile), CapTimelineEditTranscript)
	assert.Contains(t, string(profile), "不得从源毫秒自行换算")
}

func TestMediaCapabilityAdvertisesDisplayNameRename(t *testing.T) {
	var found bool
	for _, definition := range Definitions() {
		if definition.Capability.ID != CapTimelineEditMedia {
			continue
		}
		found = true
		schema, err := json.Marshal(definition.Capability.InputSchema)
		require.NoError(t, err)
		assert.Contains(t, string(schema), "mediaPatch")
		assert.Contains(t, string(schema), "displayName")
		assert.Contains(t, definition.Capability.Description, "updateMedia")
		assert.Contains(t, definition.Capability.Description, "mediaPatch.displayName")
	}
	require.True(t, found, "media capability must be registered")
	assert.True(t, editMediaAllowed["updateMedia"])
}
