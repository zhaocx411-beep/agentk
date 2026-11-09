package tools

import (
	"testing"

	"agentcut/timeline"
	"github.com/stretchr/testify/assert"
)

func TestEditClipsAllowsTransitionOperations(t *testing.T) {
	assert.True(t, editClipsAllowed[timeline.OpAddTransition])
	assert.True(t, editClipsAllowed[timeline.OpUpdateTransition])
	assert.True(t, editClipsAllowed[timeline.OpRemoveTransition])
	assert.Contains(t, descEditClips, "addTransition")
	assert.Contains(t, descEditTracks, "ducking")
	assert.Contains(t, descSetCaptions, "captionStyle")
}
