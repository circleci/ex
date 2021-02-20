package o11y

import (
	"context"
	"errors"
	"fmt"
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

func TestAddResultToSpan(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		result  string
		error   string
		warning string
	}{
		{
			name:    "all-good",
			err:     nil,
			result:  "success",
			error:   "",
			warning: "",
		},
		{
			name:    "normal-error",
			err:     errors.New("my error"),
			result:  "error",
			error:   "my error",
			warning: "",
		},
		{
			name:    "do-not-trace",
			err:     NewWarning("handled error"),
			result:  "success",
			error:   "",
			warning: "handled error",
		},
		{
			name:    "wrapped-do-not-trace",
			err:     fmt.Errorf("wrapped: %w", NewWarning("warning error (odd pair of words)")),
			result:  "success",
			error:   "",
			warning: "wrapped: warning error (odd pair of words)",
		},
		{
			name:    "context-canceled",
			err:     context.Canceled,
			result:  "canceled",
			error:   "",
			warning: "context canceled",
		},
		{
			name:    "wrapped-context-canceled",
			err:     fmt.Errorf("wrapped: %w", context.Canceled),
			result:  "canceled",
			error:   "",
			warning: "wrapped: context canceled",
		},
		{
			name:    "deadline-exceeded",
			err:     context.DeadlineExceeded,
			result:  "canceled",
			error:   "",
			warning: "context deadline exceeded",
		},
		{
			name:    "wrapped-deadline-exceeded",
			err:     fmt.Errorf("wrapped: %w", context.DeadlineExceeded),
			result:  "canceled",
			error:   "",
			warning: "wrapped: context deadline exceeded",
		},
	}

	checkField := func(span *fakeSpan, key, expect string) {
		if expect != "" {
			gotResult := span.fields[key].(string)
			assert.Equal(t, expect, gotResult)
		} else {
			_, ok := span.fields[key]
			assert.Assert(t, !ok)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := newFakeSpan()
			AddResultToSpan(span, tt.err)
			checkField(span, "result", tt.result)
			checkField(span, "error", tt.error)
			checkField(span, "warning", tt.warning)
		})
	}
}

func newFakeSpan() *fakeSpan {
	return &fakeSpan{fields: map[string]interface{}{}}
}

type fakeSpan struct {
	Span
	fields map[string]interface{}
}

func (s *fakeSpan) AddRawField(key string, val interface{}) {
	s.fields[key] = val
}
