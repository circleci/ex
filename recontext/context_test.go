package recontext

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestContextValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), "testkey", "testvalue")
	derivedCtx, cancel := WithNewDeadline(ctx, time.Now())
	defer cancel()
	assert.Check(t, cmp.Equal(derivedCtx.Value("testkey"), "testvalue"))
}

func TestNewContextExpiration(t *testing.T) {
	oldDeadline := time.Now().Add(-time.Minute)
	oldCtx, cancel := context.WithDeadline(context.Background(), oldDeadline)
	defer cancel()

	t.Run("deadline", func(t *testing.T) {
		newDeadline := time.Now().Add(time.Minute)
		derivedCtx, _ := WithNewDeadline(oldCtx, newDeadline)

		actualDeadline, _ := derivedCtx.Deadline()
		assert.Check(t, cmp.Equal(actualDeadline, newDeadline))
		assert.Check(t, actualDeadline != oldDeadline)
	})

	t.Run("timeout", func(t *testing.T) {
		now := time.Now()
		timeout := time.Second * 100
		derivedCtx, _ := WithNewTimeout(oldCtx, timeout)

		actualDeadline, _ := derivedCtx.Deadline()
		expectedDeadline := now.Add(timeout)
		delta := actualDeadline.Sub(expectedDeadline)
		assert.Check(t, delta < time.Millisecond,
			fmt.Sprintf("real deadline: %v must be within 1ms since the expected deadline: %v, is %v",
				actualDeadline, expectedDeadline, delta))
		assert.Check(t, actualDeadline != oldDeadline)
	})
}

func TestNewContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	derivedCtx, derivedCancel := WithNewTimeout(ctx, time.Second*10)

	t.Run("both contexts active", func(t *testing.T) {
		assert.Check(t, !isDone(ctx))
		assert.Check(t, !isDone(derivedCtx))

		assert.Check(t, cmp.Nil(ctx.Err()))
		assert.Check(t, cmp.Nil(derivedCtx.Err()))
	})

	cancel()

	t.Run("original context done, derived active", func(t *testing.T) {
		assert.Check(t, isDone(ctx))
		assert.Check(t, !isDone(derivedCtx))

		assert.Check(t, cmp.ErrorContains(ctx.Err(), "context canceled"), "original context cancelled")
		assert.Check(t, cmp.Nil(derivedCtx.Err()), "derived context not cancelled")
	})

	derivedCancel()
	assert.Check(t, cmp.Error(derivedCtx.Err(), "context canceled"), "derived context cancelled")
}

func isDone(ctx context.Context) bool {
	done := false
	select {
	case <-ctx.Done():
		done = true
	default:
		done = false
	}
	return done
}
