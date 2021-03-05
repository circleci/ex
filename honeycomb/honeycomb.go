// Package honeycomb implements o11y tracing.
package honeycomb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	Metrics       o11y.MetricsProvider
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

const metricKey = "__MAGIC_METRIC_KEY__"

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

	if conf.Metrics != nil {
		bc.PresendHook = extractAndSendMetrics(conf.Metrics)
	} else {
		bc.PresendHook = stripMetrics
	}

	beeline.Init(bc)

	return &honeycomb{}
}

func stripMetrics(fields map[string]interface{}) {
	delete(fields, metricKey)
}

func extractAndSendMetrics(mp o11y.MetricsProvider) func(map[string]interface{}) {
	return func(fields map[string]interface{}) {
		metrics, ok := fields[metricKey].([]o11y.Metric)
		if !ok {
			return
		}
		delete(fields, metricKey)
		for _, m := range metrics {
			switch m.Type {
			case o11y.MetricTimer:
				_ = mp.TimeInMilliseconds(
					m.Name,
					fields[m.Field].(float64),
					extractTagsFromFields(m.TagFields, fields),
					1,
				)
			case o11y.MetricCount:
				val, ok := fields[m.Field].(int64)
				if !ok {
					val = 1
				}
				tags := extractTagsFromFields(m.TagFields, fields)
				if m.FixedTag != nil {
					tags = append(tags, fmtTag(m.FixedTag.Name, m.FixedTag.Value))
				}
				_ = mp.Count(
					m.Name,
					val,
					tags,
					1,
				)
			case o11y.MetricGauge:
				val, ok := fields[m.Field].(float64)
				if !ok {
					continue
				}
				_ = mp.Gauge(
					m.Name,
					val,
					extractTagsFromFields(m.TagFields, fields),
					1,
				)
			}
		}
	}
}

func extractTagsFromFields(tags []string, fields map[string]interface{}) []string {
	result := make([]string, 0, len(tags))
	for _, name := range tags {
		val, ok := fields[name]
		if !ok {
			// Also support the app. prefix, for interop with honeycomb's prefixed fields
			val, ok = fields["app."+name]
		}
		if ok {
			result = append(result, fmtTag(name, val))
		}
	}
	return result
}

func fmtTag(name string, val interface{}) string {
	return fmt.Sprintf("%s:%v", name, val)
}

func (h *honeycomb) AddGlobalField(key string, val interface{}) {
	mustValidateKey(key)
	client.AddField(key, val)
}

func (h *honeycomb) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	ctx, s := beeline.StartSpan(ctx, name)
	return ctx, WrapSpan(s)
}

func (h *honeycomb) GetSpan(ctx context.Context) o11y.Span {
	return WrapSpan(trace.GetSpanFromContext(ctx))
}

func (h *honeycomb) AddField(ctx context.Context, key string, val interface{}) {
	mustValidateKey(key)
	beeline.AddField(ctx, key, val)
}

func (h *honeycomb) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	mustValidateKey(key)
	beeline.AddFieldToTrace(ctx, key, val)
}

func (h *honeycomb) Log(ctx context.Context, name string, fields ...o11y.Pair) {
	_, s := beeline.StartSpan(ctx, name)
	hcSpan := WrapSpan(s)
	for _, field := range fields {
		hcSpan.AddField(field.Key, field.Value)
	}
	hcSpan.End()
}

func (h *honeycomb) Close(_ context.Context) {
	beeline.Close()
}

func WrapSpan(s *trace.Span) o11y.Span {
	return &span{span: s}
}

type span struct {
	span    *trace.Span
	metrics []o11y.Metric
}

func (s *span) AddField(key string, val interface{}) {
	mustValidateKey(key)
	if err, ok := val.(error); ok {
		val = err.Error()
	}
	s.span.AddField("app."+key, val)
}

func (s *span) AddRawField(key string, val interface{}) {
	mustValidateKey(key)
	if err, ok := val.(error); ok {
		val = err.Error()
	}
	s.span.AddField(key, val)
}

func (s *span) RecordMetric(metric o11y.Metric) {
	s.metrics = append(s.metrics, metric)
	// Stash the metrics list as a span field, the pre-send hook will fish it out
	s.span.AddField(metricKey, s.metrics)
}

func (s *span) End() {
	s.span.Send()
}

func mustValidateKey(key string) {
	if strings.Contains(key, "-") {
		panic(fmt.Errorf("key %q cannot contain '-'", key))
	}
}
