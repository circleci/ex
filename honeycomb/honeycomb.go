package honeycomb

import (
	"context"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/trace"

	"github.com/circleci/distributor/o11y"
)

type honeyClient struct{}

func New(dataset, key string, stdout bool) *honeyClient {
	beeline.Init(beeline.Config{
		WriteKey: key,
		Dataset:  dataset,
		STDOUT:   stdout,
	})

	return &honeyClient{}
}

func (c *honeyClient) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	ctx, span := beeline.StartSpan(ctx, name)
	return ctx, &honeySpan{span: span}
}

func (c *honeyClient) AddField(ctx context.Context, key string, val interface{}) {
	beeline.AddField(ctx, key, val)
}

func (c *honeyClient) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	beeline.AddFieldToTrace(ctx, key, val)
}

func (c *honeyClient) Close(_ context.Context) {
	beeline.Close()
}

type honeySpan struct {
	span *trace.Span
}

func (s *honeySpan) End() {
	s.span.Send()
}
