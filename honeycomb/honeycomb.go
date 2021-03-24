// Package honeycomb implements o11y tracing.
package honeycomb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/dynsampler-go"
	libhoney "github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/libhoney-go/transmission"

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
	if s := c.sender(); s == nil {
		return errors.New("no honeycomb sender configured")
	}
	return nil
}

// sender returns the transmission.Sender to handle events to based on Format and SampleTraces.
func (c *Config) sender() transmission.Sender {
	writer := c.Writer
	if writer == nil {
		writer = os.Stderr
	}

	s := &MultiSender{}

	if c.SendTraces {
		s.Senders = append(s.Senders, &transmission.Honeycomb{
			MaxBatchSize:         libhoney.DefaultMaxBatchSize,
			BatchTimeout:         libhoney.DefaultBatchTimeout,
			MaxConcurrentBatches: libhoney.DefaultMaxConcurrentBatches,
			PendingWorkCapacity:  libhoney.DefaultPendingWorkCapacity,
			UserAgentAddition:    libhoney.UserAgentAddition,
		})
	}

	switch c.Format {
	case "text":
		s.Senders = append(s.Senders, &TextSender{w: writer})
	case "colour", "color":
		s.Senders = append(s.Senders, &TextSender{w: writer, colour: true})
	case "json":
		fallthrough
	default:
		s.Senders = append(s.Senders, &transmission.WriterSender{W: writer})
	}

	return s
}

const metricKey = "__MAGIC_METRIC_KEY__"

// New creates a new honeycomb o11y provider, which emits traces to STDOUT
// and optionally also sends them to a honeycomb server
func New(conf Config) o11y.Provider {

	// error is ignored in default constructor in beeline, so we do the same here.
	client, _ := libhoney.NewClient(libhoney.ClientConfig{
		APIKey:       conf.Key,
		Dataset:      conf.Dataset,
		APIHost:      conf.Host,
		Transmission: conf.sender(),
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
			tags := extractTagsFromFields(m.TagFields, fields)
			switch m.Type {
			case o11y.MetricTimer:
				val, ok := getField(m.Field, fields)
				if !ok {
					continue
				}
				valFloat, ok := toMilliSecond(val)
				if !ok {
					panic(m.Field + " can not be coerced to milliseconds")
				}
				_ = mp.TimeInMilliseconds(m.Name, valFloat, tags, 1)
			case o11y.MetricCount:
				var valInt int64 = 1
				if m.Field != "" {
					val, ok := getField(m.Field, fields)
					if !ok {
						continue
					}
					valInt, ok = toInt64(val)
					if !ok {
						panic(m.Field + " can not be coerced to int")
					}
				}
				if m.FixedTag != nil {
					tags = append(tags, fmtTag(m.FixedTag.Name, m.FixedTag.Value))
				}
				_ = mp.Count(m.Name, valInt, tags, 1)
			case o11y.MetricGauge:
				val, ok := getField(m.Field, fields)
				if !ok {
					continue
				}
				valFloat, ok := toFloat64(val)
				if !ok {
					panic(m.Field + " can not be coerced to float")
				}
				_ = mp.Gauge(m.Name, valFloat, tags, 1)
			}
		}
	}
}

func extractTagsFromFields(tags []string, fields map[string]interface{}) []string {
	result := make([]string, 0, len(tags))
	for _, name := range tags {
		val, ok := getField(name, fields)
		if ok {
			result = append(result, fmtTag(name, val))
		}
	}
	return result
}

func getField(name string, fields map[string]interface{}) (interface{}, bool) {
	val, ok := fields[name]
	if !ok {
		// Also support the app. prefix, for interop with honeycomb's prefixed fields
		val, ok = fields["app."+name]
	}
	return val, ok
}

func toInt64(val interface{}) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func toFloat64(val interface{}) (float64, bool) {
	if i, ok := val.(float64); ok {
		return i, true
	}
	if i, ok := toInt64(val); ok {
		return float64(i), true
	}
	return 0, false
}

func toMilliSecond(val interface{}) (float64, bool) {
	if f, ok := toFloat64(val); ok {
		return f, true
	}
	d, ok := val.(time.Duration)
	if !ok {
		p, ok := val.(*time.Duration)
		if !ok {
			return 0, false
		}
		d = *p
	}
	return float64(d.Milliseconds()), true
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
