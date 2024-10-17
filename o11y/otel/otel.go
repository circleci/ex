// Package otel contains an o11y.Provider that emits open telemetry gRPC.
// N.B. This has not been tried against a production collector, so we need to
// try it out on a safe / non production traffic service.
package otel

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel/texttrace"
)

type Config struct {
	Dataset            string
	GrpcHostAndPort    string
	HTTPHostAndPort    string
	ResourceAttributes []attribute.KeyValue

	SampleTraces  bool
	SampleKeyFunc func(map[string]any) string
	SampleRates   map[string]uint

	// DisableText prevents output to stdout for noisy services. Ignored if no other no hosts are supplied
	DisableText bool

	Test bool

	Writer  io.Writer
	Metrics o11y.ClosableMetricsProvider
}

type Provider struct {
	metricsProvider o11y.ClosableMetricsProvider
	tracer          trace.Tracer
	tp              *sdktrace.TracerProvider
}

func New(conf Config) (o11y.Provider, error) {
	var exporters []sdktrace.SpanExporter

	if conf.GrpcHostAndPort != "" {
		grpc, err := newGRPC(context.Background(), conf.GrpcHostAndPort, conf.Dataset)
		if err != nil {
			return nil, err
		}

		exporters = append(exporters, grpc)
	}

	if conf.HTTPHostAndPort != "" {
		http, err := newHTTP(context.Background(), conf.HTTPHostAndPort, conf.Dataset)
		if err != nil {
			return nil, err
		}

		exporters = append(exporters, http)
	}

	var sampler *deterministicSampler
	if conf.SampleTraces {
		sampler = &deterministicSampler{
			sampleKeyFunc: conf.SampleKeyFunc,
			sampleRates:   conf.SampleRates,
		}
	}

	if !conf.DisableText || len(exporters) == 0 {
		// Ignore disable text if no other exports defined
		out := conf.Writer
		if out == nil {
			out = os.Stdout
		}

		text, err := texttrace.New(out)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, text)
	}

	tp := traceProvider(multipleExporter{
		exporters: exporters,
		sampler:   sampler,
	}, conf)

	// set the global options
	otel.SetTracerProvider(tp)
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	otel.SetTextMapPropagator(propagator)

	// TODO check baggage is wired up above

	return &Provider{
		metricsProvider: conf.Metrics,
		tp:              tp,
		tracer:          otel.Tracer(""),
	}, nil
}

func traceProvider(exporter sdktrace.SpanExporter, conf Config) *sdktrace.TracerProvider {
	ra := append([]attribute.KeyValue{
		attribute.String("x-honeycomb-dataset", conf.Dataset),
	}, conf.ResourceAttributes...)

	res := resource.NewWithAttributes(semconv.SchemaURL, ra...)

	var sp sdktrace.SpanProcessor
	if conf.Test {
		sp = sdktrace.NewSimpleSpanProcessor(exporter)
	} else {
		sp = sdktrace.NewBatchSpanProcessor(exporter)
	}

	traceOptions := []sdktrace.TracerProviderOption{
		sdktrace.WithSpanProcessor(sp),
		// N.B. must pass in the address here since we need to see later mutations
		sdktrace.WithSpanProcessor(&globalFields),
		sdktrace.WithResource(res),
	}

	return sdktrace.NewTracerProvider(traceOptions...)
}

func newHTTP(ctx context.Context, endpoint, dataset string) (*otlptrace.Exporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
		// This header may be used by honeycomb ingestion pathways in the future, but
		// it is not currently needed for how the collectors are currently set up, which
		// expect a resource attribute instead.
		otlptracehttp.WithHeaders(map[string]string{"x-honeycomb-dataset": dataset}),
	}
	return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
}

func newGRPC(ctx context.Context, endpoint, dataset string) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
		// This header may be used by honeycomb ingestion pathways in the future, but
		// it is not currently needed for how the collectors are currently set up, which
		// expect a resource attribute instead.
		otlptracegrpc.WithHeaders(map[string]string{"x-honeycomb-dataset": dataset}),
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

type spanCtxKey struct{}

// RawProvider satisfies an interface the helpers need
func (o *Provider) RawProvider() *Provider {
	return o
}

func (o Provider) AddGlobalField(key string, val any) {
	mustValidateKey(key)
	globalFields.addField(key, val)
}

func (o Provider) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	ctx, span := o.tracer.Start(ctx, name)

	s := o.wrapSpan(span, o.getSpan(ctx))
	if s != nil {
		ctx = context.WithValue(ctx, spanCtxKey{}, s)
	}

	return ctx, s
}

// GetSpan returns the active span in the given context. It will return nil if there is no span available.
func (o Provider) GetSpan(ctx context.Context) o11y.Span {
	s := o.getSpan(ctx) // N.B returning s would mean the returned interface is not nil
	if s == nil {
		return nil
	}
	return s
}

// getSpan returns the active span in the given context. It will return nil if there is no span available.
func (o Provider) getSpan(ctx context.Context) *span {
	if s, ok := ctx.Value(spanCtxKey{}).(*span); ok {
		return s
	}
	return nil
}

func (o Provider) AddField(ctx context.Context, key string, val any) {
	s := o.GetSpan(ctx)
	if s != nil {
		s.AddField(key, val)
	}
}

func (o Provider) AddFieldToTrace(ctx context.Context, key string, val any) {
	s := o.getSpan(ctx)
	if s != nil {
		s.tr.addField(key, val)
	}
}

func (o Provider) Log(ctx context.Context, name string, fields ...o11y.Pair) {
	ctx, s := o.StartSpan(ctx, name)
	for _, f := range fields {
		s.AddField(f.Key, f.Value)
	}
	s.End()
}

func (o Provider) Close(ctx context.Context) {
	// TODO Handle these errors in a sensible manner where possible
	_ = o.tp.Shutdown(ctx)
	if o.metricsProvider != nil {
		_ = o.metricsProvider.Close()
	}
}

func (o Provider) MetricsProvider() o11y.MetricsProvider {
	return o.metricsProvider
}

func (o Provider) Helpers(disableW3c ...bool) o11y.Helpers {
	d := false
	if len(disableW3c) > 0 {
		d = disableW3c[0]
	}

	return helpers{
		p:          o,
		disableW3c: d,
	}
}

func (o Provider) wrapSpan(s trace.Span, p *span) *span {
	if s == nil {
		return nil
	}
	sp := &span{
		metricsProvider: o.metricsProvider,
		parent:          p,
		span:            s,
		start:           time.Now(),
		fields:          map[string]any{},
	}
	if p == nil {
		sp.tr = &tr{
			fields: map[string]any{},
		}
	} else {
		sp.tr = p.tr
		if p.flattenPrefix != "" {
			sp.flatten("", 0)
		}
	}
	return sp
}

type tr struct {
	mu     sync.RWMutex // mu is a write mutex for the map below (concurrent reads are safe)
	fields map[string]any
}

func (t *tr) addField(key string, val any) {
	if t == nil {
		return
	}
	// chuck out nil values
	if val == nil {
		return
	}
	mustValidateKey(key)

	t.mu.Lock()
	t.fields[key] = val
	t.mu.Unlock()
}

type span struct {
	tr              *tr
	parent          *span
	flattenPrefix   string
	flattenDepth    int
	span            trace.Span
	metrics         []o11y.Metric
	metricsProvider o11y.ClosableMetricsProvider
	start           time.Time

	mu     sync.RWMutex // mu is a write mutex for the map below (concurrent reads are safe)
	fields map[string]any
}

func (s *span) AddField(key string, val any) {
	s.AddRawField("app."+key, val)
}

func (s *span) AddRawField(key string, val any) {
	if s == nil {
		return
	}
	// chuck out nil values
	if val == nil {
		return
	}
	mustValidateKey(key)

	s.mu.Lock()
	s.fields[key] = val
	s.mu.Unlock()

	if err, ok := val.(error); ok {
		// s.span.RecordError() TODO - maybe this
		val = err.Error()
	}
	// Use otel SetName if we are overriding the name attribute
	// TODO - should we set the name attribute as well
	if key == "name" {
		if v, ok := val.(string); ok {
			s.span.SetName(v)
		}
	}

	s.span.SetAttributes(attr(key, val))
}

// RecordMetric will only emit a metric if End is called specifically
func (s *span) RecordMetric(metric o11y.Metric) {
	s.metrics = append(s.metrics, metric)
}

func (s *span) End() {
	// insert the expected field for any timing metric
	s.mu.Lock()
	s.fields["duration_ms"] = time.Since(s.start) / time.Millisecond
	s.mu.Unlock()

	if s.tr != nil {
		s.tr.mu.RLock()
		for k, v := range s.tr.fields {
			s.AddField(k, v)
		}
		s.tr.mu.RUnlock()
	}

	s.sendMetric()

	// If this span was asked to be flattened, add its fields to the parent, and don't end the span
	if s.flattenPrefix != "" {
		if s.parent != nil {
			for k, v := range s.fields {
				s.parent.AddRawField(fmt.Sprintf("%s.%s", s.flattenPrefix, k), v)
			}
		}
		return
	}
	s.span.End()
}

func (s *span) Flatten(prefix string) {
	s.flatten(prefix, 0)
}

func (s *span) flatten(prefix string, depth int) {
	flattenDepth := depth
	if s.parent != nil {
		flattenDepth = s.parent.flattenDepth
	}
	s.flattenDepth = flattenDepth + 1
	if prefix == "" {
		prefix = fmt.Sprintf("l%d", s.flattenDepth)
	}
	s.flattenPrefix = prefix

	s.AddRawField("flattened", true)
}

func (s *span) sendMetric() {
	if s.metricsProvider == nil {
		return
	}
	extractAndSendMetrics(s.metricsProvider)(s.metrics, s.snapshotFields())
}

func (s *span) snapshotFields() map[string]any {
	res := map[string]any{}
	s.mu.RLock()
	defer s.mu.RUnlock()

	for k, v := range s.fields {
		res[k] = v
	}
	return res
}

func mustValidateKey(key string) {
	if strings.Contains(key, "-") {
		panic(fmt.Errorf("key %q cannot contain '-'", key))
	}
}

type multipleExporter struct {
	exporters []sdktrace.SpanExporter
	sampler   *deterministicSampler
}

func (m multipleExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	spans = m.sampleSpans(spans)
	for _, e := range m.exporters {
		if err := e.ExportSpans(ctx, spans); err != nil {
			return err
		}
	}
	return nil
}

func (m multipleExporter) Shutdown(ctx context.Context) error {
	for _, e := range m.exporters {
		if err := e.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m multipleExporter) sampleSpans(spans []sdktrace.ReadOnlySpan) []sdktrace.ReadOnlySpan {
	if m.sampler == nil {
		return spans
	}
	ss := make([]sdktrace.ReadOnlySpan, 0, len(spans))
	for _, s := range spans {
		if ok, rate := m.sampler.shouldSample(s); ok {
			ss = append(ss, sampleRateSpan{ReadOnlySpan: s, rate: rate})
		}
	}
	return ss
}

type sampleRateSpan struct {
	sdktrace.ReadOnlySpan
	rate uint
}

func (s sampleRateSpan) Attributes() []attribute.KeyValue {
	rate := int(s.rate) //nolint:gosec
	return append(s.ReadOnlySpan.Attributes(), attribute.Int("SampleRate", rate))
}
