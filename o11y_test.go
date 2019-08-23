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

	ctx, span := StartSpan(ctx, "start-span")
	if got != "start-span" {
		t.Error("start span wired up wrong", got)
	}

	span.End()
	if got != "span-end" {
		t.Error("span end wired up wrong", got)
	}

	AddField(ctx, "fkey", "fval")
	if got != "span-fkey-fval" {
		t.Error("add field wired up wrong", got)
	}

	AddFieldToTrace(ctx, "fkey", "fval")
	if got != "aftt-fkey-fval" {
		t.Error("add field to trace wired up wrong", got)
	}

	Close(ctx)
	if got != "close" {
		t.Error("close wired up wrong", got)
	}
}

type mockClient struct {
	cb func(string)
}

func (c *mockClient) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	c.cb(name)
	return ctx, &mockSpan{cb: c.cb}
}

func (c *mockClient) AddField(ctx context.Context, key string, val interface{}) {
	c.cb(fmt.Sprintf("span-%s-%v", key, val))
}

func (c *mockClient) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	c.cb(fmt.Sprintf("aftt-%s-%v", key, val))
}

func (c *mockClient) Close(ctx context.Context) {
	c.cb("close")
}

type mockSpan struct {
	cb func(string)
}

func (s *mockSpan) End() {
	s.cb("span-end")
}
