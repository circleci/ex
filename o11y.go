package o11y

import (
	"context"
)

type Provider interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
	AddField(ctx context.Context, key string, val interface{})
	AddFieldToTrace(ctx context.Context, key string, val interface{})
	Close(ctx context.Context)
}

type providerKey struct{}

func WithProvider(ctx context.Context, p Provider) context.Context {
	return context.WithValue(ctx, providerKey{}, p)
}

func FromContext(ctx context.Context) Provider {
	provider, ok := ctx.Value(providerKey{}).(Provider)
	if !ok {
		return nil
	}
	return provider
}

type Span interface {
	End()
}

func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return FromContext(ctx).StartSpan(ctx, name)
}

func AddField(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddField(ctx, key, val)
}

func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddFieldToTrace(ctx, key, val)
}

func Close(ctx context.Context) {
	FromContext(ctx).Close(ctx)
}
