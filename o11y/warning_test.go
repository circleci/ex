package o11y

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestWarning(t *testing.T) {
	msg := "a managed error string"
	expected := msg

	var err error

	origErr := NewWarning(msg)
	warning := &wrapWarnError{}
	assert.Check(t, errors.As(origErr, &warning))
	assert.Check(t, cmp.Equal(origErr.Error(), expected))
	assert.Check(t, IsWarning(origErr))

	err = fmt.Errorf("some other error: %w", origErr)
	assert.Check(t, errors.As(err, &warning), "one wrap")
	assert.Check(t, errors.Is(err, origErr))
	assert.Check(t, cmp.ErrorContains(err, expected))
	assert.Check(t, IsWarning(err))

	err = fmt.Errorf("another error: %w", err)
	assert.Check(t, errors.As(err, &warning), "two wraps")
	assert.Check(t, errors.Is(err, origErr))
	assert.Check(t, cmp.ErrorContains(err, expected))
}

func TestWarning_TwoWarningsNotIs(t *testing.T) {
	err1 := NewWarning("warning 1")
	err2 := NewWarning("warning 2")

	assert.Check(t, !errors.Is(err1, err2))
}

func TestDontErrorTrace(t *testing.T) {
	err := NewWarning("warn")
	warning := &wrapWarnError{}
	assert.Check(t, errors.As(err, &warning))
	assert.Check(t, DontErrorTrace(err))

	err = fmt.Errorf("wrapped: %w", context.DeadlineExceeded)
	assert.Check(t, DontErrorTrace(err))

	err = fmt.Errorf("wrapped: %w", context.Canceled)
	assert.Check(t, DontErrorTrace(err))
}
