package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"agentk/internal/deltabuf"
	"agentk/internal/types"
)

const streamPingInterval = 15 * time.Second

func (s *Server) handleStreamEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requirePrincipal(w, r)
	if !ok {
		return
	}
	execs, err := s.service.ListExecutions(r.Context(), p, r.PathValue("sessionID"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	var exec types.Execution
	for i := len(execs) - 1; i >= 0; i-- {
		if execs[i].Status == types.ExecutionStatusRunning {
			exec = execs[i]
			break
		}
	}
	active := exec.ID != ""
	if !active {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	stream, err := s.deltas.Open(r.Context(), exec.ID)
	if errors.Is(err, deltabuf.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		log.Printf("api: stream %s: open buffer: %v", exec.ID, err)
		writeError(w, http.StatusServiceUnavailable, "stream unavailable")
		return
	}
	defer func() { _ = stream.Close() }()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// AccessProxy/OpenResty 默认开启 proxy_buffering；该 header 要求 Nginx 逐帧转发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		recvCtx, cancel := context.WithTimeout(r.Context(), streamPingInterval)
		item, err := stream.Recv(recvCtx)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
			continue
		}
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			return
		}
		if err != nil {
			log.Printf("api: stream %s: receive: %v", exec.ID, err)
			return
		}
		if err := writeSSEFrame(w, item.Kind, string(item.EventID), item.Data); err != nil {
			return
		}
		flusher.Flush()
		if item.Kind == deltabuf.StreamEndKind {
			return
		}
	}
}

// writeSSEFrame 仅为持久化事件写入 SSE id；delta 和 stream.end 没有事件 ID。
func writeSSEFrame(w io.Writer, kind, eventID string, payload json.RawMessage) error {
	data := []byte("null")
	if len(payload) > 0 {
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, payload); err != nil {
			return fmt.Errorf("compact %s payload: %w", kind, err)
		}
		data = compacted.Bytes()
	}
	var buf bytes.Buffer
	if eventID != "" {
		fmt.Fprintf(&buf, "id: %s\n", eventID)
	}
	fmt.Fprintf(&buf, "event: %s\ndata: %s\n\n", kind, data)
	_, err := w.Write(buf.Bytes())
	return err
}
