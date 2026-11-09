package render

import (
	"testing"

	"agentcut/timeline"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSelection(t *testing.T) {
	for _, tc := range []struct {
		executor string
		profile  string
		want     Selection
	}{
		{"", "", Selection{Executor: ExecutorLocal, Profile: DefaultLocalProfile}},
		{"local", "custom-v2", Selection{Executor: ExecutorLocal, Profile: "custom-v2"}},
		{"agentk", "", Selection{Executor: ExecutorAgentK, Profile: DefaultAgentKProfile}},
	} {
		got, err := ParseSelection(tc.executor, tc.profile)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got)
	}
	for _, invalid := range []string{"kess", "remote", "LOCALISH"} {
		_, err := ParseSelection(invalid, "")
		require.Error(t, err)
	}
}

func TestEffectiveSelectionLegacyInput(t *testing.T) {
	assert.Equal(t,
		Selection{Executor: ExecutorAgentK, Profile: LegacyAgentKProfile},
		EffectiveSelection(Input{}),
	)
	assert.Equal(t,
		Selection{Executor: ExecutorLocal, Profile: DefaultLocalProfile},
		EffectiveSelection(Input{Executor: ExecutorLocal}),
	)
}

func TestRenderKeyIncludesSchemaExecutorAndProfile(t *testing.T) {
	assert.Equal(t, "agentcut-render-v4", RenderSchemaVersion)
	doc := fixtureDoc()
	base, err := RenderKey(doc, Selection{Executor: ExecutorLocal, Profile: "p1"})
	require.NoError(t, err)
	same, err := RenderKey(doc, Selection{Executor: ExecutorLocal, Profile: "p1"})
	require.NoError(t, err)
	otherExecutor, err := RenderKey(doc, Selection{Executor: ExecutorAgentK, Profile: "p1"})
	require.NoError(t, err)
	otherProfile, err := RenderKey(doc, Selection{Executor: ExecutorLocal, Profile: "p2"})
	require.NoError(t, err)
	assert.Equal(t, base, same)
	assert.NotEqual(t, base, otherExecutor)
	assert.NotEqual(t, base, otherProfile)
	legacyV2, err := KeyOf(struct {
		Schema   string       `json:"schema"`
		Executor ExecutorKind `json:"executor"`
		Profile  string       `json:"profile"`
		Doc      any          `json:"doc"`
	}{
		Schema: "agentcut-render-v2", Executor: ExecutorLocal, Profile: "p1", Doc: doc,
	})
	require.NoError(t, err)
	assert.NotEqual(t, legacyV2, base)
}

func TestSelectForDocFallsBackToLocalOrFailsClosed(t *testing.T) {
	doc := fixtureDoc()
	doc.Items[0].FadeInFrames = 1
	configured := Selection{Executor: ExecutorAgentK, Profile: "remote"}

	got, err := SelectForDoc(configured, true, doc)
	require.NoError(t, err)
	assert.Equal(t, Selection{Executor: ExecutorLocal, Profile: DefaultLocalProfile}, got)

	_, err = SelectForDoc(configured, false, doc)
	var unsupported *UnsupportedRenderFeatureError
	require.ErrorAs(t, err, &unsupported)

	legacy := timeline.NewDoc("plain", 1920, 1080, 30)
	got, err = SelectForDoc(configured, true, legacy)
	require.NoError(t, err)
	assert.Equal(t, configured, got)

	_, err = SelectForDoc(
		Selection{Executor: ExecutorLocal, Profile: DefaultLocalProfile},
		false,
		legacy,
	)
	require.ErrorAs(t, err, &unsupported)
}

func TestLocalRenderActivityRetryPolicyIsBounded(t *testing.T) {
	options := localRenderActivityOptions()
	require.NotNil(t, options.RetryPolicy)
	assert.Equal(t, int32(3), options.RetryPolicy.MaximumAttempts)
	assert.Contains(t, options.RetryPolicy.NonRetryableErrorTypes, "LOCAL_RENDER_INVALID")
	assert.Contains(t, options.RetryPolicy.NonRetryableErrorTypes, "RENDER_SOURCE_REJECTED")
}
