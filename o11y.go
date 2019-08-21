package o11y

import (
	"context"
)

var provider Provider

type Provider interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
	AddField(ctx context.Context, key string, val interface{})
	AddFieldToTrace(ctx context.Context, key string, val interface{})
	Close(ctx context.Context)
}

func SetClient(p Provider) {
	provider = p
}

type Span interface {
	End()
}

func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return provider.StartSpan(ctx, name)
}

func AddField(ctx context.Context, key string, val interface{}) {
	provider.AddField(ctx, key, val)
}

func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	provider.AddFieldToTrace(ctx, key, val)
}

func Close(ctx context.Context) {
	provider.Close(ctx)
}
