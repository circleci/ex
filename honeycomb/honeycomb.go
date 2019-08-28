package honeycomb

import (
	"context"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"

	"github.com/circleci/distributor/o11y"
)

type honeycomb struct{}

// New creates a new honeycomb o11y provider, which emits traces to
// a honeycomb server.
func New(dataset, key, host string, stdout bool) *honeycomb {
	beeline.Init(beeline.Config{
		WriteKey: key,
		Dataset:  dataset,
		APIHost:  host,
		STDOUT:   stdout,
	})

	return &honeycomb{}
}

func (h *honeycomb) AddGlobalField(key string, val interface{}) {
	client.AddField(key, val)
}

func (h *honeycomb) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	ctx, s := beeline.StartSpan(ctx, name)
	return ctx, &span{span: s}
}

func (h *honeycomb) AddField(ctx context.Context, key string, val interface{}) {
	beeline.AddField(ctx, key, val)
}

func (h *honeycomb) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	beeline.AddFieldToTrace(ctx, key, val)
}

func (h *honeycomb) Close(_ context.Context) {
	beeline.Close()
}

type span struct {
	span *trace.Span
}

func (s *span) End() {
	s.span.Send()
}
