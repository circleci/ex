package o11y

import "context"

var client Client

type Client interface {
	StartSpan(ctx context.Context, name string) (context.Context, Span)
	AddField(ctx context.Context, key string, val interface{})
	AddFieldToTrace(ctx context.Context, key string, val interface{})
	Close(ctx context.Context)
}

func SetClient(c Client) {
	client = c
}

type Span interface {
	End()
}

func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return client.StartSpan(ctx, name)
}

func AddField(ctx context.Context, key string, val interface{}) {
	client.AddField(ctx, key, val)
}

func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	client.AddFieldToTrace(ctx, key, val)
}

func Close(ctx context.Context) {
	client.Close(ctx)
}
