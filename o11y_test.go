package o11y

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFromContext(t *testing.T) {
	t.Run("no provider", func(t *testing.T) {
		ctx := context.Background()
		p := FromContext(ctx)
		assert.Equal(t, p, defaultProvider)
	})

	t.Run("with provider in context", func(t *testing.T) {
		expected := &noopProvider{}
		ctx := WithProvider(context.Background(), expected)

		actual := FromContext(ctx)
		assert.Equal(t, actual, expected)
	})
}

func TestStartSpan_WithoutProvider(t *testing.T) {
	ctx := context.Background()

	nCtx, span := StartSpan(ctx, "foo")
	assert.Assert(t, span != nil, "should have returned a noop span")
	assert.Equal(t, ctx, nCtx, "should have returned ctx unmodified")
}

func TestHandlePanic(t *testing.T) {
	t.Run("handling panic should return error with panic wrapped", func(t *testing.T) {
		ctx := context.Background()
		var err error
		dummyPanic := func(f func()) {
			defer func() {
				x := recover()
				err = HandlePanic(ctx, FromContext(ctx).GetSpan(ctx), x, nil)
			}()
			f()
		}

		dummyPanic(func() { panic("oh no") })
		assert.ErrorContains(t, err, "oh no")
	})
}
