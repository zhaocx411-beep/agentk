package temporal

import (
	"context"
	"testing"

	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"google.golang.org/protobuf/encoding/protowire"
)

type testHeaderCarrier map[string]*commonpb.Payload

func (carrier testHeaderCarrier) Set(key string, value *commonpb.Payload) {
	carrier[key] = value
}

func (carrier testHeaderCarrier) Get(key string) (*commonpb.Payload, bool) {
	payload, ok := carrier[key]
	return payload, ok
}

func (carrier testHeaderCarrier) ForEachKey(handler func(string, *commonpb.Payload) error) error {
	for key, payload := range carrier {
		if err := handler(key, payload); err != nil {
			return err
		}
	}
	return nil
}

func TestCarrierPropagatorRoundTripPreservesOpaqueFields(t *testing.T) {
	raw := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "dev_example")
	raw = protowire.AppendVarint(protowire.AppendTag(raw, 99, protowire.VarintType), 7)
	ctx, err := ksrpc.ContextWithCarrier(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	carrier := testHeaderCarrier{}
	if err := (CarrierPropagator{}).Inject(ctx, carrier); err != nil {
		t.Fatal(err)
	}
	extracted, err := (CarrierPropagator{}).Extract(context.Background(), carrier)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := ksrpc.CarrierFromContext(extracted)
	if err != nil || !ok || string(got) != string(raw) {
		t.Fatalf("carrier = %x, %t, %v", got, ok, err)
	}
	if lane, ok, err := ksrpc.LaneFromContext(extracted); err != nil || !ok || lane != "dev_example" {
		t.Fatalf("lane = %q, %t, %v", lane, ok, err)
	}
}

func TestCarrierPropagatorLeavesUncoloredHeaderAbsent(t *testing.T) {
	carrier := testHeaderCarrier{}
	if err := (CarrierPropagator{}).Inject(context.Background(), carrier); err != nil {
		t.Fatal(err)
	}
	if _, ok := carrier[CarrierHeaderKey]; ok {
		t.Fatal("uncolored injection wrote a carrier header")
	}
}

func TestCarrierPropagatorRejectsMalformedHeaders(t *testing.T) {
	tests := map[string]any{
		"truncated":    []byte{0x0a, 0xff},
		"invalid lane": protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "dev lane"),
		"wrong type":   "dev_example",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			payload, err := converter.GetDefaultDataConverter().ToPayload(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = (CarrierPropagator{}).Extract(context.Background(), testHeaderCarrier{CarrierHeaderKey: payload})
			if err == nil {
				t.Fatal("expected extraction error")
			}
		})
	}
}
