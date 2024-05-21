// Package honeycomb implements o11y tracing.
package honeycomb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/client"
	"github.com/honeycombio/beeline-go/propagation"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/dynsampler-go"
	"github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/libhoney-go/transmission"

	"github.com/circleci/ex/o11y"
)

type honeycomb struct {
	metricsProvider o11y.ClosableMetricsProvider
}

type Config struct {
	Host          string
	Dataset       string
	Key           string
	Format        string
	SendTraces    bool // Should we actually send the traces to the honeycomb server?
	Sender        transmission.Sender
	SampleTraces  bool
	SampleKeyFunc func(map[string]interface{}) string
	SampleRates   map[string]int
	Writer        io.Writer
	Metrics       o11y.ClosableMetricsProvider
	ServiceName   string

	Debug bool
}

func (c *Config) Validate() error {
	// The key is only needed when sending traces is on and when using the default Sender
	if c.SendTraces && c.Key == "" && c.Sender == nil {
		return errors.New("honeycomb_key key required for honeycomb")
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
		if c.Sender == nil {
			s.Senders = append(s.Senders, &transmission.Honeycomb{
				MaxBatchSize:         libhoney.DefaultMaxBatchSize,
				BatchTimeout:         libhoney.DefaultBatchTimeout,
				MaxConcurrentBatches: libhoney.DefaultMaxConcurrentBatches,
				PendingWorkCapacity:  libhoney.DefaultPendingWorkCapacity,
				UserAgentAddition:    c.ServiceName,
			})
		} else {
			s.Senders = append(s.Senders, c.Sender)
		}
	}

	switch c.Format {
	case "text":
		s.Senders = append(s.Senders, &TextSender{w: writer})
	case "colour", "color":
		s.Senders = append(s.Senders, &TextSender{w: writer, colour: true})
	case "none":
		break
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
		Client:      client,
		Debug:       conf.Debug,
		WriteKey:    conf.Key,
		ServiceName: conf.ServiceName,
	}

	if conf.SampleTraces {
		if conf.SampleRates == nil {
			conf.SampleRates = map[string]int{}
		}
		// See beeline.Config.SamplerHook
		sampler := &TraceSampler{
			KeyFunc: conf.SampleKeyFunc,
			Sampler: &dynsampler.Static{
				Default: 1,
				Rates:   conf.SampleRates,
			},
		}

		bc.SamplerHook = func(fields map[string]interface{}) (bool, int) {
			// NB: We prepare and send metrics here in case the span is dropped
			// due to sampling. If a span is sampled, the PresendHook is not invoked.
			extractAndSendMetrics(conf.Metrics)(fields)
			return sampler.Hook(fields)
		}
	}

	// in the case that we're not sampling, we will attempt to send metrics
	// as part of the event PresendHook instead.
	if bc.SamplerHook == nil {
		bc.PresendHook = extractAndSendMetrics(conf.Metrics)
	}

	beeline.Init(bc)

	return &honeycomb{
		metricsProvider: conf.Metrics,
	}
}

func stripMetrics(fields map[string]interface{}) {
	delete(fields, metricKey)
}

func extractAndSendMetrics(mp o11y.MetricsProvider) func(map[string]interface{}) {
	if mp == nil {
		// if there is no configured provider, simply strip the metrics
		return func(fields map[string]interface{}) {
			stripMetrics(fields)
		}
	}

	return func(fields map[string]interface{}) {
		standardErrorMetrics(mp, fields)

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

func standardErrorMetrics(mp o11y.MetricsProvider, fields map[string]interface{}) {
	// detect and map the fail same errors and add a metric for it if found
	failClass := addFailure(fields)
	if failClass != "" {
		_ = mp.Count("failure", 1, []string{fmtTag("class", failClass)}, 1)
	}
	// add standard metric for error and warning
	tag := []string{fmtTag("type", "o11y")}
	if _, ok := fields["error"]; ok {
		_ = mp.Count("error", 1, tag, 1)
	}
	if _, ok := fields["warning"]; ok {
		_ = mp.Count("warning", 1, tag, 1)
	}
}

// addFailure finds the first field suffixed with _error and adds the prefix as the value
// to a failure field, if there is not already a failure field, and returns the prefix.
// The original _error field is kept to retain details of its value.
// If found the prefix part is returned.
func addFailure(fields map[string]interface{}) string {
	if _, ok := fields["failure"]; ok {
		return ""
	}
	for k := range fields {
		errClass := strings.TrimSuffix(k, "_error")
		if errClass != k {
			fields["failure"] = errClass
			return errClass
		}
	}
	return ""
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
	span := trace.GetSpanFromContext(ctx)
	var newSpan *trace.Span
	if span != nil {
		ctx, newSpan = span.CreateAsyncChild(ctx)
	} else {
		// there is no trace active; we should make one, but use the root span
		// as the "new" span instead of creating a child of this mostly empty
		// span
		ctx, _ = trace.NewTrace(ctx, nil)
		newSpan = trace.GetSpanFromContext(ctx)
	}
	newSpan.AddField("name", name)

	return ctx, WrapSpan(newSpan)
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
	if h.metricsProvider != nil {
		_ = h.metricsProvider.Close()
	}
}

func (h *honeycomb) MetricsProvider() o11y.MetricsProvider {
	return h.metricsProvider
}

func (h *honeycomb) Helpers() o11y.Helpers {
	return helpers{}
}

type helpers struct{}

func (h helpers) ExtractPropagation(ctx context.Context) o11y.PropagationContext {
	s := trace.GetSpanFromContext(ctx)
	if s == nil {
		return o11y.PropagationContext{}
	}
	parent := s.SerializeHeaders()
	return o11y.PropagationContext{
		Parent: parent,
		Headers: http.Header{
			propagation.TracePropagationHTTPHeader: []string{parent},
		},
	}
}

func (h helpers) InjectPropagation(ctx context.Context, p o11y.PropagationContext) (context.Context, o11y.Span) {
	var prop *propagation.PropagationContext

	field := p.Parent
	if field == "" {
		field = p.Headers.Get(propagation.TracePropagationHTTPHeader)
	}
	// Use the honeycomb propagation if present, otherwise grab the w3c headers
	if field != "" {
		prop, _ = propagation.UnmarshalHoneycombTraceContext(field)
	} else {
		_, prop, _ = propagation.UnmarshalW3CTraceContext(ctx, map[string]string{
			propagation.TraceparentHeader: p.Headers.Get(propagation.TraceparentHeader),
		})
	}

	ctx, tr := trace.NewTrace(ctx, prop)
	return ctx, WrapSpan(tr.GetRootSpan())
}

func (h helpers) TraceIDs(ctx context.Context) (traceID, parentID string) {
	t := trace.GetTraceFromContext(ctx)
	if t == nil {
		return "", ""
	}
	return t.GetTraceID(), t.GetParentID()
}

func WrapSpan(s *trace.Span) o11y.Span {
	if s == nil {
		return nil
	}
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
