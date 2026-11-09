package client

import (
	"connectrpc.com/connect"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
)

// EventStream receives live event frames for one Session and Execution. Call
// Receive in a loop, inspect Msg after each successful receive, and check Err
// when Receive returns false.
type EventStream struct {
	inner *connect.ServerStreamForClient[apiv1.WatchEventsResponse]
}

func newEventStream(
	inner *connect.ServerStreamForClient[apiv1.WatchEventsResponse],
) *EventStream {
	return &EventStream{inner: inner}
}

// Receive advances the stream to the next frame and reports whether a frame is
// available. When it returns false, call Err to distinguish a clean end from a
// receive failure.
func (s *EventStream) Receive() bool {
	return s.inner.Receive()
}

// Msg returns the frame from the most recent successful call to Receive. The
// returned message must not be used after the next call to Receive.
func (s *EventStream) Msg() *apiv1.WatchEventsResponse {
	return s.inner.Msg()
}

// Err returns the first non-EOF receive error after Receive returns false.
func (s *EventStream) Err() error {
	return s.inner.Err()
}

// Close stops receiving and releases the stream resources. Call Close when the
// stream is no longer needed, including when iteration ends early.
func (s *EventStream) Close() error {
	return s.inner.Close()
}
