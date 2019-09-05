package o11y

import (
	"context"
	"fmt"
	"testing"
)

func TestO11y(t *testing.T) {
	got := ""
	provider := &mockClient{cb: func(what string) {
		got = what
	}}

	ctx := WithProvider(context.Background(), provider)

	provider.AddGlobalField("version", 42)
	if got != "global-version-42" {
		t.Error("add global field wired up wrong", got)
	}

	ctx, span := StartSpan(ctx, "start-span")
	if got != "start-span" {
		t.Error("start span wired up wrong", got)
	}

	if FromContext(ctx) == nil {
		t.Error("context returned from span has dropped the provider")
	}

	span.AddField("fkey", "fval")
	if got != "span-fkey-fval" {
		t.Error("add field wired up wrong", got)
	}

	span.End()
	if got != "span-end" {
		t.Error("span end wired up wrong", got)
	}
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

type mockClient struct {
	cb func(string)
}

func (c *mockClient) AddGlobalField(key string, val interface{}) {
	c.cb(fmt.Sprintf("global-%s-%v", key, val))
}

func (c *mockClient) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	c.cb(name)
	return ctx, &mockSpan{cb: c.cb}
}

func (c *mockClient) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	c.cb(fmt.Sprintf("aftt-%s-%v", key, val))
}

func (c *mockClient) Close(ctx context.Context) {}

type mockSpan struct {
	cb func(string)
}

func (s *mockSpan) AddField(key string, val interface{}) {
	s.cb(fmt.Sprintf("span-%s-%v", key, val))
}

func (s *mockSpan) End() {
	s.cb("span-end")
}
