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

func TestWarning_Join(t *testing.T) {
	we := NewWarning("i am a warning")
	err := errors.New("i am an error")

	t.Run("one of each", func(t *testing.T) {
		te := errors.Join(we, err)
		assert.Check(t, IsWarning(te), "joined %q should be a warning", te)
	})

	t.Run("only errors", func(t *testing.T) {
		te := errors.Join(err, err)
		assert.Check(t, !IsWarning(te), "joined %q should not be a warning", te)
	})

	t.Run("only warnings", func(t *testing.T) {
		te := errors.Join(we, we)
		assert.Check(t, IsWarning(te), "joined %q should be a warning", te)
	})

	t.Run("fmt wrapped", func(t *testing.T) {
		te := fmt.Errorf("%w: %w", NewWarning("bad body"), err)
		assert.Check(t, IsWarning(te), "fmt wrapped %q should be a warning", te)
	})

	t.Run("fmt wrapped and joined", func(t *testing.T) {
		wErr := fmt.Errorf("%w: %w", NewWarning("bad body"), err)
		te := errors.Join(err, wErr)
		assert.Check(t, IsWarning(te), "fmt wrapped and joined %q should be a warning", te)
	})
}

type testError struct {
	thing int
}

func (e testError) Error() string {
	return "test error"
}

func TestNewAllWarningError(t *testing.T) {
	we := NewWarning("i am a warning")
	err := errors.New("i am an error")

	t.Run("is", func(t *testing.T) {
		te := AllWarning(errors.Join(we, err))
		assert.Check(t, errors.Is(te, we), "err %q should be is", te)
	})

	t.Run("as", func(t *testing.T) {
		te := AllWarning(errors.Join(we, testError{thing: 5}))
		hc := testError{}
		assert.Check(t, errors.As(te, &hc), "err %q should be is", te)
		assert.Check(t, cmp.Equal(hc.thing, 5))
	})

	t.Run("one of each", func(t *testing.T) {
		te := AllWarning(errors.Join(we, err))
		assert.Check(t, !IsWarning(te), "joined %q should not be a warning", te)
	})

	t.Run("only errors", func(t *testing.T) {
		te := AllWarning(errors.Join(err, err))
		assert.Check(t, !IsWarning(te), "joined %q should not be a warning", te)
	})

	t.Run("only all warnings", func(t *testing.T) {
		te := AllWarning(errors.Join(we, we))
		assert.Check(t, IsWarning(te), "joined %q should be a warning", te)
	})

	t.Run("fmt wrapped with error", func(t *testing.T) {
		te := AllWarning(fmt.Errorf("%w: %w", NewWarning("bad body"), err))
		assert.Check(t, !IsWarning(te), "fmt wrapped %q should not be a warning", te)
	})

	t.Run("fmt wrapped only warnings", func(t *testing.T) {
		te := AllWarning(fmt.Errorf("%w: %w", NewWarning("bad body"), we))
		assert.Check(t, IsWarning(te), "fmt wrapped %q should be a warning", te)
	})

	t.Run("fmt wrapped and joined", func(t *testing.T) {
		wErr := fmt.Errorf("%w: %w", NewWarning("bad body"), we)
		te := AllWarning(errors.Join(we, wErr))
		assert.Check(t, IsWarning(te), "fmt wrapped and joined %q should be a warning", te)
	})

	t.Run("single warning is a warning", func(t *testing.T) {
		te := AllWarning(we)
		assert.Check(t, IsWarning(te), "single %q should be a warning", te)
	})

	t.Run("single error is not warning", func(t *testing.T) {
		te := AllWarning(err)
		assert.Check(t, !IsWarning(te), "single %q should not a warning", te)
	})

	t.Run("nil warning is not error", func(t *testing.T) {
		te := AllWarning(nil)
		assert.Check(t, cmp.Nil(te), "single %q should be nil", te)
	})

	t.Run("nested", func(t *testing.T) {
		we2 := errors.Join(we, err)
		assert.Check(t, IsWarning(we2))

		// Since we have wrapped the we2 (containing an error) it should cause te to be reported as not a warning
		te := AllWarning(errors.Join(we, AllWarning(we2)))
		assert.Check(t, !IsWarning(te), "single %q should be a warning", te)
	})
}
