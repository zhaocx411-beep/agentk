package tool

import "testing"

func TestJSONFieldPaths(t *testing.T) {
	raw := []byte(`{"data":{"task":{"id":"t1","progress":42}},"status":"ok"}`)
	if v, ok := JSONFieldString(raw, "data.task.id"); !ok || v != "t1" {
		t.Fatalf("data.task.id = %q, %v", v, ok)
	}
	if v, ok := JSONFieldString(raw, "data.task.progress"); !ok || v != "42" {
		t.Fatalf("progress = %q, %v", v, ok)
	}
	if _, ok := JSONField(raw, "data.missing"); ok {
		t.Fatal("missing path should not resolve")
	}
	if v, ok := JSONField(raw, ""); !ok || string(v) != string(raw) {
		t.Fatal("empty path should return whole document")
	}
}
