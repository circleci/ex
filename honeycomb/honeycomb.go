package honeycomb

import (
	"context"

	"github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/libhoney-go"

	"github.com/circleci/distributor/o11y"
)

type honeycomb struct{}

// New creates a new honeycomb o11y provider, which emits JSON traces to STDOUT
// and optionally also sends them to a honeycomb server
func New(dataset, key, host string, send bool) o11y.Provider {
	// error is ignored in default constructor in beeline, so we do the same here.
	c, _ := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:       key,
		Dataset:      dataset,
		APIHost:      host,
		Transmission: newSender(send),
	})

	beeline.Init(beeline.Config{
		Client: c,
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
