package entity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateClientMutationID(t *testing.T) {
	for _, valid := range []string{"req-1", "550e8400-e29b-41d4-a716-446655440000", "a.b_c-9"} {
		require.NoError(t, ValidateClientMutationID(valid))
	}
	for _, invalid := range []string{
		"",
		" leading-space",
		"contains:colon",
		"contains/slash",
		"换行",
		strings.Repeat("a", MaxClientMutationIDLength+1),
	} {
		assert.Error(t, ValidateClientMutationID(invalid), invalid)
	}
}

func TestTimelineMutationUpdateIDIsStableAndProjectIsolated(t *testing.T) {
	first := TimelineMutationUpdateID("p1", "user", "req-1")
	assert.Equal(t, first, TimelineMutationUpdateID("p1", "user", "req-1"))
	assert.NotEqual(t, first, TimelineMutationUpdateID("p2", "user", "req-1"))
	assert.NotEqual(t, first, TimelineMutationUpdateID("p1", "agent", "req-1"))
	assert.NotContains(t, first, "p1")
	assert.NotContains(t, first, "req-1")
	assert.LessOrEqual(t, len(first), 128)
}
