package honeycomb

import (
	"context"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"
	libhoney "github.com/honeycombio/libhoney-go"

	"github.com/circleci/distributor/o11y"
)

type honeycomb struct{}

type Config struct {
	Host    string
	Dataset string
	Key     string
	// Should we actually send the traces to the honeycomb server?
	SendTraces bool
	// See beeline.Config.SamplerHook
	Sampler *TraceSampler
}

// New creates a new honeycomb o11y provider, which emits JSON traces to STDOUT
// and optionally also sends them to a honeycomb server
func New(conf Config) o11y.Provider {
	// error is ignored in default constructor in beeline, so we do the same here.
	c, _ := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:       conf.Key,
		Dataset:      conf.Dataset,
		APIHost:      conf.Host,
		Transmission: newSender(conf.SendTraces),
	})

	bc := beeline.Config{
		Client: c,
	}
	if conf.Sampler != nil {
		bc.SamplerHook = conf.Sampler.Hook
	}

	beeline.Init(bc)

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

func (h *honeycomb) Log(ctx context.Context, name string, fields ...o11y.Pair) {
	_, s := beeline.StartSpan(ctx, name)
	hcSpan := &span{span: s}
	for _, field := range fields {
		hcSpan.AddField(field.Key, field.Value)
	}
	hcSpan.End()
}

func (h *honeycomb) Close(_ context.Context) {
	beeline.Close()
}

type span struct {
	span *trace.Span
}

func (s *span) AddField(key string, val interface{}) {
	s.span.AddField("app."+key, val)
}

func (s *span) End() {
	s.span.Send()
}
