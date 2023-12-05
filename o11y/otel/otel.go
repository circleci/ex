// Package otel contains an o11y.Provider that emits open telemetry gRPC.
// N.B. This has not been tried against a production collector, so we need to
// try it out on a safe / non production traffic service.
package otel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel/texttrace"
)

type OTel struct {
	metricsProvider o11y.ClosableMetricsProvider
	tracer          trace.Tracer
	tp              *sdktrace.TracerProvider
}

func New(conf Config) (o11y.Provider, error) {
	globalFields.addField("service.name", conf.Service)

	var exporter sdktrace.SpanExporter

	exporter, err := texttrace.New(os.Stdout)
	if err != nil {
		return nil, err
	}
	if conf.GrpcHostAndPort != "" {
		grpc, err := newGRPC(context.Background(), conf.GrpcHostAndPort)
		if err != nil {
			return nil, err
		}
		// use gRPC and text - mainly so acceptance testing can harvest the ports.
		exporter = multipleExporter{
			exporters: []sdktrace.SpanExporter{
				exporter,
				grpc,
			},
		}
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(conf.Service),
		semconv.ServiceVersionKey.String(conf.Version),
	)

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithSpanProcessor(globalFields),
		sdktrace.WithResource(res),
	)

	// set the global options
	otel.SetTracerProvider(tp)
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{}, propagation.TraceContext{})
	otel.SetTextMapPropagator(propagator)

	// TODO check baggage is wired up above

	mProv, err := metricsProvider(conf.Config)
	if err != nil {
		return nil, fmt.Errorf("metrics provider failed: %w", err)
	}

	return &OTel{
		metricsProvider: mProv,
		tp:              tp,
		tracer:          otel.Tracer(""),
	}, nil
}

func newGRPC(ctx context.Context, endpoint string) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

var spanCtxKey = struct{}{}

func (o OTel) AddGlobalField(key string, val interface{}) {
	mustValidateKey(key)
	globalFields.addField(key, val)
}

func (o OTel) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	ctx, span := o.tracer.Start(ctx, name)

	s := o.startSpan(span)
	if s != nil {
		ctx = context.WithValue(ctx, spanCtxKey, s)
	}

	return ctx, s
}

func (o OTel) GetSpan(ctx context.Context) o11y.Span {
	if s, ok := ctx.Value(spanCtxKey).(*span); ok {
		return s
	}
	return nil
}

func (o OTel) AddField(ctx context.Context, key string, val interface{}) {
	trace.SpanFromContext(ctx).SetAttributes(attr(key, val))
}

func (o OTel) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	// TODO - some equivalent to adding this field to all child spans to the root span
	o.AddField(ctx, key, val)
}

func (o OTel) Log(ctx context.Context, name string, fields ...o11y.Pair) {
	// TODO Log
}

func (o OTel) Close(ctx context.Context) {
	// TODO Handle these errors in a sensible manner where possible
	_ = o.tp.Shutdown(ctx)
	if o.metricsProvider != nil {
		_ = o.metricsProvider.Close()
	}
}

func (o OTel) MetricsProvider() o11y.MetricsProvider {
	return o.metricsProvider
}

func (o OTel) Helpers() o11y.Helpers {
	return helpers{}
}

func (o OTel) startSpan(s trace.Span) *span {
	if s == nil {
		return nil
	}
	return &span{
		metricsProvider: o.metricsProvider,
		span:            s,
		start:           time.Now(),
		fields:          map[string]interface{}{},
	}
}

type span struct {
	span            trace.Span
	metrics         []o11y.Metric
	metricsProvider o11y.ClosableMetricsProvider
	start           time.Time
	fields          map[string]interface{}
}

func (s *span) AddField(key string, val interface{}) {
	s.AddRawField("app."+key, val)
}

func (s *span) AddRawField(key string, val interface{}) {
	mustValidateKey(key)
	s.fields[key] = val
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
	s.sendMetric()
	s.span.End()
}

func (s *span) sendMetric() {
	if s.metricsProvider == nil {
		return
	}
	// insert the expected field for any timing metric
	s.fields["duration_ms"] = time.Since(s.start) / time.Millisecond
	extractAndSendMetrics(s.metricsProvider)(s.metrics, s.fields)
}

func mustValidateKey(key string) {
	if strings.Contains(key, "-") {
		panic(fmt.Errorf("key %q cannot contain '-'", key))
	}
}

type multipleExporter struct {
	exporters []sdktrace.SpanExporter
}

func (m multipleExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
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
