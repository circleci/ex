package valueonly

import (
	"context"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestContextValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), "testkey", "testvalue")

	derivedCtx := &Context{ctx}
	assert.Equal(t, derivedCtx.Value("testkey"), "testvalue")
}

func TestContextDeadline(t *testing.T) {
	// Creating original context and deadlining it.
	ctx := context.Background()
	setDeadline := time.Now().Add(-time.Second)
	ctx, cancel := context.WithDeadline(ctx, setDeadline)
	defer cancel()

	done := false
	select {
	case <-ctx.Done():
		done = true
	default:
		done = false
	}
	actualDeadline, deadlineSet := ctx.Deadline()
	assert.Equal(t, actualDeadline, setDeadline, "original context's deadline is set")
	assert.Equal(t, deadlineSet, true, "original context's deadline is set")
	assert.Equal(t, done, true, "original context's done channel resolved")
	assert.Equal(t, ctx.Err().Error(), "context deadline exceeded",
		"original context cancelled by deadline")

	// Checking that the derived context is not cancelled.
	derivedCtx := &Context{ctx}
	derivedDone := false
	select {
	case <-derivedCtx.Done():
		derivedDone = true
	default:
		derivedDone = false
	}

	_, derivedDeadlineSet := derivedCtx.Deadline()
	assert.Equal(t, derivedDeadlineSet, false, "derived context has no deadline set")
	assert.Equal(t, derivedDone, false, "derived context's done channel not resolved")
	assert.Equal(t, derivedCtx.Err(), nil, "derived context's error is nil")

	// Checking that we can cancel the derived context.
	derivedCtxTryCancel, cancel := context.WithCancel(derivedCtx)
	cancel()
	assert.Equal(t, derivedCtxTryCancel.Err().Error(), "context canceled",
		"can cancel a derived context")
}
