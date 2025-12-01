// Package otel contains an o11y.Provider that emits open telemetry gRPC.
// N.B. This has not been tried against a production collector, so we need to
// try it out on a safe / non production traffic service.
package otel

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
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
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel/texttrace"
)

type Config struct {
	Dataset         string
	GrpcHostAndPort string

	// HTTPTracesURL configures a host for exporting traces to http[s]://host[:port][/path]
	HTTPTracesURL string

	// HTTPAuthorization is the authorization token to send with http requests
	HTTPAuthorization secret.String

	ResourceAttributes []attribute.KeyValue

	SampleTraces  bool
	SampleKeyFunc func(map[string]any) string
	SampleRates   map[string]uint

	// DisableText prevents output to stdout for noisy services. Ignored if no other no hosts are supplied
	DisableText bool

	Test bool

	Writer  io.Writer
	Metrics o11y.ClosableMetricsProvider

	// SpanExporters allows you explicitly provide a set of exporters, as an advanced use-case.
	SpanExporters []sdktrace.SpanExporter
}

type Provider struct {
	metricsProvider o11y.ClosableMetricsProvider
	tracer          trace.Tracer
	tp              *sdktrace.TracerProvider
}

func New(conf Config) (o11y.Provider, error) {
	exporters := slices.Clone(conf.SpanExporters)

	if conf.GrpcHostAndPort != "" {
		grpc, err := newGRPC(context.Background(), conf.GrpcHostAndPort)
		if err != nil {
			return nil, err
		}

		exporters = append(exporters, grpc)
	}

	if conf.HTTPTracesURL != "" {
		http, err := NewHttpExporter(conf)
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

		text, err := texttrace.New(out, conf.Test)
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

	return &Provider{
		metricsProvider: conf.Metrics,
		tp:              tp,
		tracer:          otel.Tracer(""),
	}, nil
}

// NewMetricsOnly returns a metrics only provider, to capture the span metrics behavior.
// This can be used to compose with a custom provider to access span based metrics.
func NewMetricsOnly(metrics o11y.ClosableMetricsProvider) o11y.Provider {
	return &Provider{
		metricsProvider: metrics,
		tracer:          noop.NewTracerProvider().Tracer(""),
	}
}

func NewHttpExporter(conf Config) (*otlptrace.Exporter, error) {
	var serviceName, serviceVersion string
	for _, a := range conf.ResourceAttributes {
		switch a.Key {
		case semconv.ServiceNameKey:
			serviceName = a.Value.AsString()
		case semconv.ServiceVersionKey:
			serviceVersion = a.Value.AsString()
		}
	}
	http, err := newHTTP(context.Background(), httpOpts{
		endpoint: conf.HTTPTracesURL,
		token:    conf.HTTPAuthorization,
		service:  serviceName,
		version:  serviceVersion,
	})
	return http, err
}

func traceProvider(exporter sdktrace.SpanExporter, conf Config) *sdktrace.TracerProvider {
	ra := append([]attribute.KeyValue{
		attribute.String("x-honeycomb-dataset", conf.Dataset), // TODO - remove once over to environments
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

type httpOpts struct {
	endpoint string
	token    secret.String
	service  string
	version  string
}

func newHTTP(ctx context.Context, opt httpOpts) (*otlptrace.Exporter, error) {
	ua := fmt.Sprintf("CircleCI (%s/%s, ex) via OTel OTLP Exporter Go/%s",
		opt.service, opt.version, otlptrace.Version())

	headers := map[string]string{
		"User-Agent": ua,
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(opt.endpoint)}
	if opt.token != "" {
		headers["Authorization"] = fmt.Sprintf("Bearer %s", opt.token.Raw())
	}
	opts = append(opts, otlptracehttp.WithHeaders(headers))

	return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
}

func newGRPC(ctx context.Context, endpoint string) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

type spanCtxKey struct{}

// RawProvider satisfies an interface the helpers need
func (o *Provider) RawProvider() *Provider {
	return o
}

func (o Provider) AddGlobalField(key string, val any) {
	globalFields.addField(key, val)
}

func (o Provider) StartSpan(ctx context.Context, name string, opts ...o11y.SpanOpt) (context.Context, o11y.Span) {
	so := toOtelOpts(opts)

	ctx, span := o.tracer.Start(ctx, name, so...)

	s := o.wrapSpan(name, opts, span, o.getSpan(ctx))
	if s != nil {
		ctx = context.WithValue(ctx, spanCtxKey{}, s)
	}

	return ctx, s
}

func toOtelOpts(opts []o11y.SpanOpt) []trace.SpanStartOption {
	cfg := o11y.SpanConfig{}
	for _, opt := range opts {
		cfg = opt(cfg)
	}
	if cfg.Kind == 0 {
		cfg.Kind = o11y.SpanKindInternal
	}
	var so []trace.SpanStartOption
	so = append(so, trace.WithSpanKind(trace.SpanKind(cfg.Kind)))
	return so
}

type golden struct {
	ctx  context.Context
	span *span
}

const metaGolden = "meta.golden"

func (o Provider) startGoldenTrace(ctx context.Context, name string) context.Context {
	spec := o.getGolden(ctx)
	if spec != nil {
		return ctx
	}
	// first golden span - so use a clean context for the golden root span.
	sCtx, ssp := o.tracer.Start(context.Background(), name)
	spec = &golden{
		ctx:  sCtx,
		span: o.wrapSpan(name, nil, ssp, nil),
	}
	spec.span.AddRawField(metaGolden, true)
	return context.WithValue(ctx, goldenCtxKey{}, spec)
}

func (o Provider) MakeSpanGolden(ctx context.Context) context.Context {
	// Get the existing span, and do nothing if there isn't one.
	sp := o.getSpan(ctx)
	if sp == nil {
		return ctx
	}

	spec := o.getGolden(ctx)
	if spec == nil {
		ctx = o.startGoldenTrace(ctx, "root")
		spec = o.getGolden(ctx)
	}

	// Start the golden span
	spec.ctx, _ = o.StartSpan(spec.ctx, sp.name, sp.opts...)
	sp.golden = o.getSpan(spec.ctx)
	sp.golden.AddRawField(metaGolden, true)

	return ctx
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

type goldenCtxKey struct{}

// getGolden returns the active span in the given context. It will return nil if there is no span available.
func (o Provider) getGolden(ctx context.Context) *golden {
	if s, ok := ctx.Value(goldenCtxKey{}).(*golden); ok {
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
	if o.tp != nil {
		_ = o.tp.Shutdown(ctx)
	}
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

func (o Provider) wrapSpan(name string, opts []o11y.SpanOpt, s trace.Span, p *span) *span {
	if s == nil {
		return nil
	}
	sp := &span{
		name:            name,
		opts:            opts,
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

	t.mu.Lock()
	t.fields[key] = val
	t.mu.Unlock()
}

type span struct {
	tr              *tr
	parent          *span
	flattenPrefix   string
	flattenDepth    int
	golden          *span
	span            trace.Span
	metrics         []o11y.Metric
	metricsProvider o11y.ClosableMetricsProvider
	start           time.Time

	// name and opts are needed to be able to create a matching golden span
	name string
	opts []o11y.SpanOpt

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

	s.mu.Lock()
	s.fields[key] = val

	if err, ok := val.(error); ok {
		// s.span.RecordError() TODO - maybe this
		val = err.Error()
	}
	// Use otel SetName if we are overriding the name attribute
	if key == "name" {
		if v, ok := val.(string); ok {
			s.name = val.(string)
			s.span.SetName(v)
		}
	}
	s.mu.Unlock()

	s.span.SetAttributes(attr(key, val))
}

// RecordMetric will only emit a metric if End is called specifically
func (s *span) RecordMetric(metric o11y.Metric) {
	s.metrics = append(s.metrics, metric)
}

func (s *span) End() {
	// insert the expected field for any timing metric
	s.mu.Lock()
	s.fields["duration_ms"] = time.Since(s.start).Milliseconds()
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

	// if this span has a golden span the copy over the attributes from the span and end it
	if s.golden != nil {
		s.golden.copyAttrsFrom(s)
		s.golden.End()
	}
}

// copy span attributes into s
func (s *span) copyAttrsFrom(span *span) {
	// get the span name
	var spName string
	s.mu.Lock()
	spName = s.name
	s.mu.Unlock()

	attrs := span.snapshotFields()

	span.mu.Lock()
	defer span.mu.Unlock()

	s.name = spName
	s.span.SetName(spName)
	for k, v := range attrs {
		if k == "name" || k == "duration_ms" {
			continue
		}
		s.fields[k] = v
		s.span.SetAttributes(attr(k, v))
	}
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
