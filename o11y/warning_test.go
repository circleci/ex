package o11y

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
)

func TestWarning(t *testing.T) {
	msg := "a managed error string"
	expected := msg

	var err error

	origErr := NewWarning(msg)
	warning := &wrapWarnError{}
	assert.Assert(t, errors.As(origErr, &warning))
	assert.Equal(t, origErr.Error(), expected)
	assert.Assert(t, IsWarning(origErr))

	err = fmt.Errorf("some other error: %w", origErr)
	assert.Assert(t, errors.As(err, &warning), "one wrap")
	assert.Assert(t, errors.Is(err, origErr))
	assert.ErrorContains(t, err, expected)
	assert.Assert(t, IsWarning(err))

	err = fmt.Errorf("another error: %w", err)
	assert.Assert(t, errors.As(err, &warning), "two wraps")
	assert.Assert(t, errors.Is(err, origErr))
	assert.ErrorContains(t, err, expected)
}

func TestWarning_TwoWarningsNotIs(t *testing.T) {
	err1 := NewWarning("warning 1")
	err2 := NewWarning("warning 2")

	assert.Assert(t, !errors.Is(err1, err2))
}

func TestDontErrorTrace(t *testing.T) {
	err := NewWarning("warn")
	warning := &wrapWarnError{}
	assert.Assert(t, errors.As(err, &warning))
	assert.Assert(t, DontErrorTrace(err))

	err = fmt.Errorf("wrapped: %w", context.DeadlineExceeded)
	assert.Assert(t, DontErrorTrace(err))

	err = fmt.Errorf("wrapped: %w", context.Canceled)
	assert.Assert(t, DontErrorTrace(err))
}
