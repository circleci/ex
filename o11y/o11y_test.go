package o11y

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestFromContext(t *testing.T) {
	t.Run("no provider", func(t *testing.T) {
		ctx := context.Background()
		p := FromContext(ctx)
		assert.Check(t, cmp.Equal(p, defaultProvider))
	})

	t.Run("with provider in context", func(t *testing.T) {
		expected := &noopProvider{}
		ctx := WithProvider(context.Background(), expected)

		actual := FromContext(ctx)
		assert.Check(t, cmp.Equal(actual, expected))
	})
}

func TestLog_WithoutProvider(t *testing.T) {
	ctx := context.Background()

	Log(ctx, "foo", Field("name", "value"))
}

func TestStartSpan_WithoutProvider(t *testing.T) {
	ctx := context.Background()

	nCtx, span := StartSpan(ctx, "foo")
	assert.Check(t, span != nil, "should have returned a noop span")
	assert.Check(t, cmp.Equal(ctx, nCtx), "should have returned ctx unmodified")
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
		assert.Check(t, cmp.ErrorContains(err, "oh no"))
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
			assert.Check(t, cmp.Equal(expect, gotResult))
		} else {
			_, ok := span.fields[key]
			assert.Check(t, !ok)
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

type fakeProvider struct {
	Provider
	span *fakeSpan
}

func (p *fakeProvider) StartSpan(ctx context.Context, name string, opts ...SpanOpt) (context.Context, Span) {
	span := newFakeSpan()
	p.span = span
	return ctx, span
}

func (p *fakeProvider) GetSpan(ctx context.Context) Span {
	return p.span
}

func TestSetSpanSampledIn(t *testing.T) {
	ctx := context.Background()
	ctx = WithProvider(ctx, &fakeProvider{})
	ctx, span := StartSpan(ctx, "foo")
	SetSpanSampledIn(ctx)

	fs, ok := span.(*fakeSpan)
	assert.Assert(t, ok)

	assert.Check(t, fs.fields["meta.keep.span"], true)
}

type fakeEndWithErrSpan struct {
	*fakeSpan

	endError error
}

func (s *fakeEndWithErrSpan) EndWithError(err *error) {
	if err == nil {
		s.endError = nil
		return
	}
	s.endError = *err
}

func TestEnd(t *testing.T) {
	span := &fakeEndWithErrSpan{
		fakeSpan: newFakeSpan(),
	}

	err := errors.New("oh no")

	t.Run("has error", func(t *testing.T) {
		End(span, &err)
		assert.Check(t, cmp.ErrorIs(err, span.endError))
	})

	t.Run("nil error", func(t *testing.T) {
		e := &err
		*e = nil // make the underlying error nil - the interface won't be nil
		End(span, e)
		assert.NilError(t, err)
	})

	t.Run("nil interface", func(t *testing.T) {
		End(span, nil)
		assert.NilError(t, err)
	})
}
