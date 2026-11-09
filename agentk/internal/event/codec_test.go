package event

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtures 定义当前支持的事件 JSON 格式。
const fixturesDir = "../../spec/fixtures/events"

func TestFixturesRoundTrip(t *testing.T) {
	for name, raw := range readFixtures(t) {
		t.Run(name, func(t *testing.T) {
			var ev Event
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			out, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			var want, got any
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("normalize fixture: %v", err)
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("normalize re-marshal: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round-trip mismatch\nfixture:    %s\nre-marshal: %s", raw, out)
			}
		})
	}
}

// TestFixturesCoverAllKinds 要求每个 kind 都有兼容性 fixture。
func TestFixturesCoverAllKinds(t *testing.T) {
	covered := make(map[Kind]bool)
	for name, raw := range readFixtures(t) {
		var head struct {
			Kind Kind `json:"kind"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatalf("%s: read kind: %v", name, err)
		}
		if covered[head.Kind] {
			continue
		}
		covered[head.Kind] = true
	}
	for _, kind := range AllKinds {
		if !covered[kind] {
			t.Errorf("kind %q has no fixture under %s", kind, fixturesDir)
		}
	}
	if len(covered) != len(AllKinds) {
		t.Errorf("fixtures cover %d kinds, AllKinds has %d", len(covered), len(AllKinds))
	}
}

// TestAllKindsDecodeToMatchingPayload 验证 kind、AllKinds 和解码分支一致。
func TestAllKindsDecodeToMatchingPayload(t *testing.T) {
	for _, kind := range AllKinds {
		t.Run(string(kind), func(t *testing.T) {
			p, err := UnmarshalPayload(kind, []byte(`{}`))
			if err != nil {
				t.Fatalf("decode empty %s: %v", kind, err)
			}
			if p.Kind() != kind {
				t.Fatalf("decoded payload reports kind %q, want %q", p.Kind(), kind)
			}
		})
	}
}

func TestUnmarshalPayloadRejectsUnknownKind(t *testing.T) {
	if _, err := UnmarshalPayload("assistant.singing", []byte(`{}`)); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind error = %v", err)
	}
}

func TestUnmarshalPayloadRejectsMalformedData(t *testing.T) {
	if _, err := UnmarshalPayload(KindUserInput, []byte(`{"text":`)); err == nil {
		t.Fatal("expected malformed payload to be rejected")
	}
}

func TestMarshalPayloadRejectsNil(t *testing.T) {
	if _, err := MarshalPayload(nil); err == nil {
		t.Fatal("expected nil payload to be rejected")
	}
}

// TestEventJSONOmitsRoundIDOutsideRound 验证 round 外事件不输出空 roundId。
func TestEventJSONOmitsRoundIDOutsideRound(t *testing.T) {
	raw, err := json.Marshal(Event{
		ID:          "evt_0191f38d-99f2-7d91-9f2b-712ab18e0a01",
		ExecutionID: "exe_0191f38d-99f2-7d91-9f2b-712ab18e0a00",
		Payload:     UserInput{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "roundId") {
		t.Fatalf("event = %s, want no roundId key", raw)
	}
}

func readFixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("read fixtures dir: %v", err)
	}
	out := make(map[string]json.RawMessage, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(fixturesDir, entry.Name()))
		if err != nil {
			t.Fatalf("read fixture %s: %v", entry.Name(), err)
		}
		out[entry.Name()] = raw
	}
	if len(out) == 0 {
		t.Fatalf("no fixtures found under %s", fixturesDir)
	}
	return out
}
