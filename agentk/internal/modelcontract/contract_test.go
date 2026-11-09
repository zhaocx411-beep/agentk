package modelcontract

import "testing"

func TestDefinitionsAreUniqueAndComplete(t *testing.T) {
	seen := make(map[string]bool, len(definitions))
	for _, def := range definitions {
		if def.ID == "" || def.Family == "" || def.Dialect == "" {
			t.Fatalf("incomplete definition: %+v", def)
		}
		if seen[def.ID] {
			t.Fatalf("duplicate model ID %q", def.ID)
		}
		seen[def.ID] = true
		got, ok := Lookup(def.ID)
		if !ok || got != def {
			t.Fatalf("lookup %q = %+v, %v", def.ID, got, ok)
		}
	}
}
