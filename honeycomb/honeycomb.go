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
}

func (c *Config) Validate() error {
	if c.SendTraces && c.Key == "" {
		return errors.New("honeycomb_key key required for honeycomb")
	}
	if _, err := c.ConsoleWriter(); err != nil {
		return err
	}
	return nil
}

// ConsoleWriter used to write events to a local stderr
func (c *Config) ConsoleWriter() (io.Writer, error) {
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
	writer, err := conf.ConsoleWriter()
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
	span      *trace.Span
	resultSet bool
}

func (s *span) AddField(key string, val interface{}) {
	if err, ok := val.(error); ok {
		val = err.Error()
	}
	if key == "result" {
		s.resultSet = true
	}
	s.span.AddField("app."+key, val)
}

// End takes zero or one pointers to error, any more than one error is ignored.
// If there is a non nil error passed in it will set the result.
// Otherwise if no "result" field has already been set then a
// result:success is set.
func (s *span) End(errors ...*error) {
	if len(errors) == 0 || errors[0] == nil {
		s.span.Send()
		return
	}
	// If the error is not nil add the error result.
	// Otherwise if we have not set a result already then go ahead
	if *errors[0] != nil || !s.resultSet {
		o11y.AddResultToSpan(s, *errors[0])
	}
	s.span.Send()
}
