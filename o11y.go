package o11y

import (
	"context"
)

var client Client

type Client interface {
	GetSpanFromContext(ctx context.Context) Span
	StartSpan(ctx context.Context, name string) (context.Context, Span)
	AddFieldToTrace(ctx context.Context, key string, val interface{})
	Flush(ctx context.Context)
	Close(ctx context.Context)
}

func SetClient(c Client) {
	client = c
}

type Span interface {
	AddField(key string, val interface{})
	Send()
}

func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return client.StartSpan(ctx, name)
}

func AddField(ctx context.Context, key string, val interface{}) {
	sp := client.GetSpanFromContext(ctx)
	if sp == nil {
		return
	}
	sp.AddField(key, val)
}

func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	client.AddFieldToTrace(ctx, key, val)
}

func Flush(ctx context.Context) {
	client.Flush(ctx)
}

func Close(ctx context.Context) {
	client.Close(ctx)
}
