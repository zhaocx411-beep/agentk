package gateway

import (
	"encoding/json"
	"reflect"
	"testing"

	inferpb "git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/gen/infer"
)

func TestFeatureJSONRoundTrip(t *testing.T) {
	raw := map[string]json.RawMessage{
		"text":       json.RawMessage(`"hello"`),
		"start_time": json.RawMessage(`123`),
		"threshold":  json.RawMessage(`0.3`),
		"enabled":    json.RawMessage(`true`),
		"labels":     json.RawMessage(`["a","b"]`),
		"weights":    json.RawMessage(`[0.25,1]`),
		"nested":     json.RawMessage(`{"count":2,"ok":true}`),
		"blob":       json.RawMessage(`{"$bytes":"QUJD"}`),
	}
	features, err := decodeFeatures(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := features["start_time"].GetIntVal(); got != 123 {
		t.Fatalf("start_time = %d", got)
	}
	if got := features["labels"].GetStrArray().GetStrElems(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("labels = %#v", got)
	}
	if got := features["blob"].GetBytesVal(); string(got) != "ABC" {
		t.Fatalf("blob = %q", got)
	}

	encoded := encodeFeatures(features)
	if encoded["text"] != "hello" || encoded["enabled"] != true {
		t.Fatalf("encoded = %#v", encoded)
	}
	if got := encoded["blob"].(map[string]any)["$bytes"]; got != "QUJD" {
		t.Fatalf("encoded blob = %#v", got)
	}
}

func TestDecodeFeatureRejectsNull(t *testing.T) {
	_, err := decodeFeatures(map[string]json.RawMessage{"bad": json.RawMessage(`null`)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeMediaFeature(t *testing.T) {
	feature := &inferpb.ExFeature{Kind: &inferpb.ExFeature_MediaData{MediaData: &inferpb.MediaData{
		BlobstoreKey: &inferpb.ProtoBlobStoreKey{Db: "upload", Table: "demo", Key: "x.mp4"},
	}}}
	value := featureToJSON(feature).(map[string]any)
	key := value["blobstoreKey"].(map[string]any)
	if key["db"] != "upload" || key["key"] != "x.mp4" {
		t.Fatalf("media = %#v", value)
	}
}
