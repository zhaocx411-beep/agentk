package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// CanaryResult records the object used by RunCanary. The object has already
// been deleted when RunCanary returns successfully.
type CanaryResult struct {
	Key          string
	URI          string
	ContentRange string
}

// RunCanary verifies the remote storage operations AgentCut depends on. It is
// deliberately explicit: API and Worker startup never call it automatically.
func RunCanary(
	ctx context.Context,
	store Storage,
	key string,
	payload []byte,
) (result CanaryResult, err error) {
	if store == nil {
		return result, fmt.Errorf("blob canary: storage is not configured")
	}
	if !strings.HasPrefix(key, "agentcut-e2e/") ||
		strings.Contains(key, "..") ||
		strings.ContainsAny(key, "\\\r\n") {
		return result, fmt.Errorf("blob canary: unsafe key %q", key)
	}
	if len(payload) < 4 {
		return result, fmt.Errorf("blob canary: payload must contain at least 4 bytes")
	}
	result.Key = key
	cleanupNeeded := true
	defer func() {
		if !cleanupNeeded {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if cleanupErr := store.Delete(cleanupCtx, key); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("blob canary cleanup: %w", cleanupErr))
		}
	}()

	result.URI, err = store.Put(
		ctx, key, bytes.NewReader(payload), int64(len(payload)), "text/plain",
	)
	if err != nil {
		return result, fmt.Errorf("blob canary PUT: %w", err)
	}

	object, err := store.Get(ctx, key)
	if err != nil {
		return result, fmt.Errorf("blob canary GET: %w", err)
	}
	actual, readErr := io.ReadAll(io.LimitReader(object.Body, int64(len(payload)+1)))
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil {
		return result, fmt.Errorf("blob canary GET body: %w", errors.Join(readErr, closeErr))
	}
	if !bytes.Equal(actual, payload) {
		return result, fmt.Errorf(
			"blob canary GET content mismatch: got %d bytes, want %d",
			len(actual), len(payload),
		)
	}

	rangeStore, ok := store.(RangeStorage)
	if !ok {
		return result, fmt.Errorf("blob canary: backend does not support Range GET")
	}
	ranged, err := rangeStore.GetRange(ctx, key, "bytes=1-3")
	if err != nil {
		return result, fmt.Errorf("blob canary Range GET: %w", err)
	}
	result.ContentRange = ranged.ContentRange
	rangeBody, readErr := io.ReadAll(io.LimitReader(ranged.Body, 4))
	closeErr = ranged.Body.Close()
	if readErr != nil || closeErr != nil {
		return result, fmt.Errorf(
			"blob canary Range GET body: %w",
			errors.Join(readErr, closeErr),
		)
	}
	if !bytes.Equal(rangeBody, payload[1:4]) {
		return result, fmt.Errorf("blob canary Range GET content mismatch")
	}

	if err = store.Delete(ctx, key); err != nil {
		return result, fmt.Errorf("blob canary DELETE: %w", err)
	}
	cleanupNeeded = false
	return result, nil
}
