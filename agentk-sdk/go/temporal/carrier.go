package temporal

import (
	"context"
	"fmt"

	ksrpc "git.corp.kuaishou.com/klingai-agentk/ksresolve.git/go/rpc"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

// CarrierHeaderKey is the Temporal header holding the KS trace carrier. It is
// the same key the carrier uses in gRPC metadata and HTTP headers.
const CarrierHeaderKey = "trace_context-bin"

// CarrierPropagator moves the KS trace carrier (request lane and opaque trace
// fields) between the caller's context and Temporal headers, so that the lane
// reaching a Workflow or Activity is the one the originating request carried
// and outgoing ksresolve calls from Activities forward it unchanged.
//
// A missing header leaves the context uncolored; a malformed header fails
// extraction instead of silently falling back to baseline.
type CarrierPropagator struct{}

var _ workflow.ContextPropagator = CarrierPropagator{}

type carrierKey struct{}

// LaneFromWorkflow returns the request lane carried by a Workflow context.
func LaneFromWorkflow(ctx workflow.Context) (string, bool, error) {
	raw, ok := ctx.Value(carrierKey{}).([]byte)
	if !ok {
		return "", false, nil
	}
	lane, err := ksrpc.ParseCarrier(raw)
	if err != nil {
		return "", false, err
	}
	return lane, lane != "", nil
}

// CarrierFromWorkflow returns the raw trace carrier held by a Workflow context.
func CarrierFromWorkflow(ctx workflow.Context) ([]byte, bool) {
	raw, ok := ctx.Value(carrierKey{}).([]byte)
	return raw, ok
}

func (CarrierPropagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	raw, ok, err := ksrpc.CarrierFromContext(ctx)
	if err != nil {
		return fmt.Errorf("temporal carrier: %w", err)
	}
	if !ok {
		return nil
	}
	return setHeader(writer, raw)
}

func (CarrierPropagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	raw, ok := CarrierFromWorkflow(ctx)
	if !ok {
		return nil
	}
	return setHeader(writer, raw)
}

func (CarrierPropagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	raw, present, err := readHeader(reader)
	if err != nil || !present {
		return ctx, err
	}
	result, err := ksrpc.ContextWithCarrier(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("temporal carrier: invalid header: %w", err)
	}
	return result, nil
}

func (CarrierPropagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	raw, present, err := readHeader(reader)
	if err != nil || !present {
		return ctx, err
	}
	if _, err := ksrpc.ParseCarrier(raw); err != nil {
		return nil, fmt.Errorf("temporal carrier: invalid header: %w", err)
	}
	return workflow.WithValue(ctx, carrierKey{}, raw), nil
}

func setHeader(writer workflow.HeaderWriter, raw []byte) error {
	payload, err := converter.GetDefaultDataConverter().ToPayload(raw)
	if err != nil {
		return fmt.Errorf("temporal carrier: encode header: %w", err)
	}
	writer.Set(CarrierHeaderKey, payload)
	return nil
}

func readHeader(reader workflow.HeaderReader) ([]byte, bool, error) {
	payload, present := reader.Get(CarrierHeaderKey)
	if !present {
		return nil, false, nil
	}
	var raw []byte
	if err := converter.GetDefaultDataConverter().FromPayload(payload, &raw); err != nil {
		return nil, false, fmt.Errorf("temporal carrier: decode header: %w", err)
	}
	return raw, true, nil
}
