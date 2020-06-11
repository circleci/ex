// Package honeycomb implements o11y tracing.
package honeycomb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/dynsampler-go"
	libhoney "github.com/honeycombio/libhoney-go"

	"github.com/circleci/distributor/o11y"
)

type honeycomb struct{}

type Config struct {
	Host          string
	Dataset       string
	Key           string
	Format        string
	SendTraces    bool // Should we actually send the traces to the honeycomb server?
	SampleTraces  bool
	SampleKeyFunc func(map[string]interface{}) string
	Writer        io.Writer
}

func (c *Config) Validate() error {
	if c.SendTraces && c.Key == "" {
		return errors.New("honeycomb_key key required for honeycomb")
	}
	if _, err := c.writer(); err != nil {
		return err
	}
	return nil
}

// writer returns the writer as given by the config or returns
// a stderr writer to write events to based on the config Format.
func (c *Config) writer() (io.Writer, error) {
	if c.Writer != nil {
		return c.Writer, nil
	}
	switch c.Format {
	case "json":
		return os.Stderr, nil
	case "text":
		return DefaultTextFormat, nil
	case "colour", "color":
		return ColourTextFormat, nil
	}
	return nil, fmt.Errorf("unknown format: %s", c.Format)
}

// New creates a new honeycomb o11y provider, which emits traces to STDOUT
// and optionally also sends them to a honeycomb server
func New(conf Config) o11y.Provider {
	writer, err := conf.writer()
	if err != nil {
		writer = ColourTextFormat
	}

	// error is ignored in default constructor in beeline, so we do the same here.
	client, _ := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:       conf.Key,
		Dataset:      conf.Dataset,
		APIHost:      conf.Host,
		Transmission: newSender(writer, conf.SendTraces),
	})

	bc := beeline.Config{
		Client: client,
	}

	if conf.SampleTraces {
		// See beeline.Config.SamplerHook
		sampler := &TraceSampler{
			KeyFunc: conf.SampleKeyFunc,
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
