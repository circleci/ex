package o11y

import (
	"context"
	"testing"

	"gotest.tools/assert"
)

func TestFromContext(t *testing.T) {
	t.Run("no provider", func(t *testing.T) {
		ctx := context.Background()
		p := FromContext(ctx)
		assert.Equal(t, p, nil)
	})

	t.Run("with provider in context", func(t *testing.T) {
		expected := &fakeProvider{}
		ctx := WithProvider(context.Background(), expected)

		actual := FromContext(ctx)
		assert.Equal(t, actual, expected)
	})
}

func TestStartSpan_WithoutProvider(t *testing.T) {
	ctx := context.Background()
	p := FromContext(ctx)
	if p != nil {
		t.Error("no provider on context should have returned nil")
	}

	nCtx, span := StartSpan(ctx, "foo")
	if span != nil {
		t.Error("should not have got a span if there is no provider on the context")
	}
	if ctx != nCtx {
		t.Error("context should be equal if no provider present")
	}
}

type fakeProvider struct{}

func (c *fakeProvider) AddGlobalField(key string, val interface{}) {}

func (c *fakeProvider) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, &mockSpan{}
}

func (c *fakeProvider) AddField(ctx context.Context, key string, val interface{}) {}

func (c *fakeProvider) AddFieldToTrace(ctx context.Context, key string, val interface{}) {}

func (c *fakeProvider) Close(ctx context.Context) {}

type mockSpan struct{}

func (s *mockSpan) AddField(key string, val interface{}) {}

func (s *mockSpan) End() {}
