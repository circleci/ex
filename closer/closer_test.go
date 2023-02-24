package closer

import (
	"errors"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestErrorHandler(t *testing.T) {
	t.Run("with error", func(t *testing.T) {
		var errorSentinel = errors.New("error sentinel")

		called := false
		closer := func() error {
			called = true
			return errorSentinel
		}
		var err error
		ErrorHandler(closerFunc(closer), &err)
		assert.Check(t, called)
		assert.Check(t, cmp.ErrorIs(err, errorSentinel))
	})

	t.Run("no error", func(t *testing.T) {
		called := false
		closer := func() error {
			called = true
			return nil
		}
		var err error
		ErrorHandler(closerFunc(closer), &err)
		assert.Check(t, called)
		assert.Check(t, err)
	})
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}
