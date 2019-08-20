package honeycomb

import (
	"context"
	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/trace"

	"github.com/circleci/distributor/o11y"
)

type honeyClient struct {
}

func New(host string, stdout bool) *honeyClient {
	beeline.Init(beeline.Config{
		WriteKey: "YOUR_API_KEY",
		Dataset:  "distributor",
		APIHost:  host,
		STDOUT:   stdout,
	})

	return &honeyClient{}
}

func (c *honeyClient) GetSpanFromContext(ctx context.Context) o11y.Span {
	return trace.GetSpanFromContext(ctx)
}

func (c *honeyClient) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	return beeline.StartSpan(ctx, name)
}

func (c *honeyClient) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	beeline.AddFieldToTrace(ctx, key, val)
}

func (c *honeyClient) Flush(ctx context.Context) {
	beeline.Flush(ctx)
}

func (c *honeyClient) Close(_ context.Context) {
	beeline.Close()
}
