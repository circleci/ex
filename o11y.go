// Package o11y provides observability in the form of tracing and metrics
package o11y

import (
	"context"
	"errors"
)

var ErrDoNotTrace = errors.New("this error should not be treated as an error in trace reporting")

type Provider interface {
	// AddGlobalField adds data which should apply to every span in the application
	//
	// eg. version, service, k8s_replicaset
	AddGlobalField(key string, val interface{})

	// StartSpan begins a new span that'll represent a unit of work
	//
	// `name` should be a short human readable identifier of the work.
	// It can and should include some details to distinguish it from other
	// similar spans - like the URL or the DB query name.
	//
	// The caller is responsible for calling End(), usually via defer:
	//
	//   ctx, span := o11y.StartSpan(ctx, "GET /help")
	//   defer span.End()
	StartSpan(ctx context.Context, name string) (context.Context, Span)

	// GetSpan returns the currently active span
	GetSpan(ctx context.Context) Span

	// AddField is for adding application-level information to the currently active span
	//
	// Any field name will be prefixed with "app."
	AddField(ctx context.Context, key string, val interface{})

	// AddFieldToTrace is for adding useful information to the root span.
	//
	// This will be propagated onto every child span.
	//
	// eg. build-url, plan-id, project-id, org-id etc
	AddFieldToTrace(ctx context.Context, key string, val interface{})

	// Log sends a zero duration trace event.
	Log(ctx context.Context, name string, fields ...Pair)

	Close(ctx context.Context)
}

type Span interface {
	// AddField is for adding application-level information to the span
	//
	// Any field name will be prefixed with "app."
	AddField(key string, val interface{})

	// AddRawField is for adding useful information to the span in library/plumbing code
	// Generally application code should prefer AddField() to avoid namespace clashes
	//
	// eg. result, http.status_code, db.system etc
	//
	// Refer to the opentelemetry draft spec for naming inspiration
	// https://github.com/open-telemetry/opentelemetry-specification/tree/7ae3d066c95c716ef3086228ef955d84ba03ac88/specification/trace/semantic_conventions
	AddRawField(key string, val interface{})

	// RecordMetric tells the provider to emit a metric to its metric backend when the span ends
	RecordMetric(metric Metric)

	// End sets the duration of the span and tells the related provider that the span is complete
	// so it can do it's appropriate processing. The span should not be used after End is called.
	End()
}

type MetricType string

const (
	MetricTimer = "timer"
)

type Metric struct {
	Type MetricType
	// Name is the metric name that will be emitted
	Name string
	// Field is the span field to use as the metric's value
	Field string
	// TagFields are additional span fields to use as metric tags
	TagFields []string
}

func Timing(name string, fields ...string) Metric {
	return Metric{MetricTimer, name, "duration_ms", fields}
}

type MetricsProvider interface {
	TimeInMilliseconds(name string, value float64, tags []string, rate float64) error
}

type providerKey struct{}

// WithProvider returns a child context which contains the Provider. The Provider
// can be retrieved with FromContext.
func WithProvider(ctx context.Context, p Provider) context.Context {
	return context.WithValue(ctx, providerKey{}, p)
}

// FromContext returns the provider stored in the context, or nil if none exists.
func FromContext(ctx context.Context) Provider {
	provider, ok := ctx.Value(providerKey{}).(Provider)
	if !ok {
		return defaultProvider
	}
	return provider
}

// StartSpan starts a span from a context that must contain a provider for this to have any effect.
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return FromContext(ctx).StartSpan(ctx, name)
}

// AddField adds a field to the currently active span
func AddField(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddField(ctx, key, val)
}

// AddFieldToTrace adds a field to the currently active root span and all of its current and future child spans
func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddFieldToTrace(ctx, key, val)
}

// End completes a span, including using AddResultToSpan to set the error and result fields
//
// The correct way to capture the returned error is given in the doc example, it is like this..
// defer o11y.End(span, &err)
//
// Using the unusual pointer to the interface means that clients can call defer on End early,
// typically on the next line after calling StartSpan as it will capture the address of the named
// return error at that point. Any further assignments are made to the pointed to data, so that when
// our End func dereferences the pointer we get the last assigned error as desired.
func End(span Span, err *error) {
	var actualErr error
	if err != nil {
		actualErr = *err
	}
	AddResultToSpan(span, actualErr)
	span.End()
}

// AddResultToSpan takes a possibly nil error, and updates the "error" and "result" fields of the span appropriately.
func AddResultToSpan(span Span, err error) {
	if errors.Is(err, ErrDoNotTrace) {
		err = nil
	}

	if err != nil {
		span.AddRawField("result", "error")
		span.AddRawField("error", err.Error())
		return
	}

	span.AddRawField("result", "success")
}

// Pair is a key value pair used to add metadata to a span.
type Pair struct {
	Key   string
	Value interface{}
}

// Field returns a new metadata pair.
func Field(key string, value interface{}) Pair {
	return Pair{Key: key, Value: value}
}

var defaultProvider = &noopProvider{}

type noopProvider struct{}

func (c *noopProvider) AddGlobalField(key string, val interface{}) {}

func (c *noopProvider) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, &noopSpan{}
}
func (c *noopProvider) GetSpan(ctx context.Context) Span {
	return &noopSpan{}
}

func (c *noopProvider) AddField(ctx context.Context, key string, val interface{}) {}

func (c *noopProvider) AddFieldToTrace(ctx context.Context, key string, val interface{}) {}

func (c *noopProvider) Close(ctx context.Context) {}

func (c *noopProvider) Log(ctx context.Context, name string, fields ...Pair) {}

type noopSpan struct{}

func (s *noopSpan) AddField(key string, val interface{}) {}

func (s *noopSpan) AddRawField(key string, val interface{}) {}

func (s *noopSpan) RecordMetric(metric Metric) {}

func (s *noopSpan) End() {}
