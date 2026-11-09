package agent

import "testing"

func TestEffectiveMaxToolRounds(t *testing.T) {
	if got := EffectiveMaxToolRounds(0); got != DefaultMaxToolRounds {
		t.Fatalf("0 → %d, want %d", got, DefaultMaxToolRounds)
	}
	if got := EffectiveMaxToolRounds(-3); got != DefaultMaxToolRounds {
		t.Fatalf("negative → %d, want %d", got, DefaultMaxToolRounds)
	}
	if got := EffectiveMaxToolRounds(7); got != 7 {
		t.Fatalf("7 → %d, want 7", got)
	}
}
