package poll

import (
	"context"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

type it func() (stop bool, err error)

// AssertIt will periodically call it up to duration. It is a function that returns
// a bool to stop the polling, and a resultant error. This function will assert that
// no error was returned.
func AssertIt(ctx context.Context, t *testing.T, duration time.Duration, it it) {
	t.Helper()
	err := ForIt(ctx, duration, it)
	assert.NilError(t, err)
}

// ForIt will periodically call it up to duration. It is a function that returns
//// a bool to stop the polling, and a resultant error.
func ForIt(ctx context.Context, duration time.Duration, it it) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stop, err := it()
		if stop {
			return err
		}
		time.Sleep(time.Millisecond * 50)
	}
}
