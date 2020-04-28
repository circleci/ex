package honeycomb

import (
	"context"
	"fmt"
	"io"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/dynsampler-go"
	libhoney "github.com/honeycombio/libhoney-go"

	"github.com/circleci/distributor/o11y"
)

type honeycomb struct{}

// New creates a new honeycomb o11y provider, which emits traces to STDOUT
// and optionally also sends them to a honeycomb server
func New(c Config) o11y.Provider {
	var writer io.Writer = ColourTextFormat
	if c.Format != nil {
		writer = c.Format.Value()
	}

	// error is ignored in default constructor in beeline, so we do the same here.
	client, _ := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:       c.HoneycombKey.Value(),
		Dataset:      c.HoneycombDataset,
		APIHost:      c.Host,
		Transmission: newSender(writer, c.HoneycombEnabled),
	})

	bc := beeline.Config{
		Client: client,
	}

	if c.SampleTraces {
		sampler := &TraceSampler{
			KeyFunc: func(fields map[string]interface{}) string {
				return fmt.Sprintf("%s %s %d",
					fields["app.server_name"],
					fields["request.path"],
					fields["response.status_code"],
				)
			},
			Sampler: &dynsampler.Static{
				Default: 1,
				Rates:   map[string]int{},
			},
		}
		bc.SamplerHook = sampler.Hook
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
	if err, ok := val.(error); ok {
		val = err.Error()
	}
	s.span.AddField("app."+key, val)
}

func (s *span) End() {
	s.span.Send()
}
