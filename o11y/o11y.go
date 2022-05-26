// Package o11y provides observability in the form of tracing and metrics
package o11y

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rollbar/rollbar-go"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"github.com/DataDog/datadog-go/statsd"
)

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

	// MetricsProvider grants lower control over the metrics that o11y sends, allowing skipping spans.
	MetricsProvider() MetricsProvider
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

	SerializeHeaders() string
}

type MetricType string

const (
	MetricTimer = "timer"
	MetricGauge = "gauge"
	MetricCount = "count"
)

type Metric struct {
	Type MetricType `json:"metricType"`
	// Name is the metric name that will be emitted
	Name string `json:"name"`
	// Field is the span field to use as the metric's value
	Field string `json:"field"`
	// FixedTag is an optional tag added at Metric definition time
	FixedTag *Tag `json:"fixedTag"`
	// TagFields are additional span fields to use as metric tags
	TagFields []string `json:"tagFields"`
}

type Tag struct {
	Name  string
	Value interface{}
}

func NewTag(name string, value interface{}) *Tag {
	return &Tag{Name: name, Value: value}
}

func Timing(name string, fields ...string) Metric {
	return Metric{Type: MetricTimer, Name: name, Field: "duration_ms", TagFields: fields}
}

func Duration(name string, valueField string, fields ...string) Metric {
	return Metric{Type: MetricTimer, Name: name, Field: valueField, TagFields: fields}
}

func Incr(name string, fields ...string) Metric {
	return Metric{Type: MetricCount, Name: name, TagFields: fields}
}

func Gauge(name string, valueField string, tagFields ...string) Metric {
	return Metric{
		Type:      MetricGauge,
		Name:      name,
		Field:     valueField,
		TagFields: tagFields,
	}
}

func Count(name string, valueField string, fixedTag *Tag, tagFields ...string) Metric {
	return Metric{
		Type:      MetricCount,
		Name:      name,
		Field:     valueField,
		FixedTag:  fixedTag,
		TagFields: tagFields,
	}
}

type MetricsProvider interface {
	TimeInMilliseconds(name string, value float64, tags []string, rate float64) error
	// Gauge measures the value of a metric at a particular time.
	Gauge(name string, value float64, tags []string, rate float64) error
	// Count sends an individual value in time.
	Count(name string, value int64, tags []string, rate float64) error
}

type ClosableMetricsProvider interface {
	MetricsProvider
	io.Closer
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

// Log sends a zero duration trace event.
func Log(ctx context.Context, name string, fields ...Pair) {
	FromContext(ctx).Log(ctx, name, fields...)
}

// LogError sends a zero duration trace event with an error.
func LogError(ctx context.Context, name string, err error, fields ...Pair) {
	_, span := StartSpan(ctx, name)
	for _, f := range fields {
		span.AddField(f.Key, f.Value)
	}
	AddResultToSpan(span, err)
	span.End()
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
	switch {
	case IsWarning(err):
		span.AddRawField("warning", err.Error())
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		// Context cancellation and timeouts are expected, for instance in timeout and shutdown scenarios.
		// Tracing as an error adds clutter when looking for real errors.
		span.AddRawField("result", "canceled")
		span.AddRawField("warning", err.Error())
		return
	case err != nil:
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

// Baggage is a map of values used for telemetry purposes.
// See: https://github.com/open-telemetry/opentelemetry-specification/blob/14b5b6a944e390e368dd2e2ef234d220d8287d19/specification/baggage/api.md
type Baggage map[string]string

// AddToTrace adds all entries in the Baggage to the root span.
func (b Baggage) addToTrace(ctx context.Context) {
	o := FromContext(ctx)
	for k, v := range b {
		k := strings.ReplaceAll(k, "-", "_")
		o.AddFieldToTrace(ctx, k, v)
	}
}

type baggageKey struct{}

func WithBaggage(ctx context.Context, baggage Baggage) context.Context {
	baggage.addToTrace(ctx)
	return context.WithValue(ctx, baggageKey{}, baggage)
}

func GetBaggage(ctx context.Context) Baggage {
	b, ok := ctx.Value(baggageKey{}).(Baggage)
	if !ok {
		return Baggage{}
	}
	return b
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

func (c *noopProvider) MetricsProvider() MetricsProvider {
	return &statsd.NoOpClient{}
}

type noopSpan struct{}

func (s *noopSpan) SerializeHeaders() string {
	return ""
}

func (s *noopSpan) AddField(key string, val interface{}) {}

func (s *noopSpan) AddRawField(key string, val interface{}) {}

func (s *noopSpan) RecordMetric(metric Metric) {}

func (s *noopSpan) End() {}

func HandlePanic(ctx context.Context, span Span, panic interface{}, r *http.Request) (err error) {
	err = fmt.Errorf("panic handled: %+v", panic)
	span.AddRawField("panic", panic)
	span.AddRawField("has_panicked", "true")
	span.AddRawField("stack", string(debug.Stack()))
	span.RecordMetric(Incr("panics", "name"))

	provider := FromContext(ctx)
	rollable, ok := provider.(rollbarAble)
	if !ok {
		return err
	}
	rollbarClient := rollable.RollBarClient()
	if r != nil {
		rollbarClient.RequestError(rollbar.CRIT, r, err)
	} else {
		rollbarClient.LogPanic(panic, true)
	}
	return err
}

type rollbarAble interface {
	RollBarClient() *rollbar.Client
}

// Scan satisfies the `Scanner` interface to allow the database driver to un-marshall
// it back into a struct from the JSON blob in the database.
func (b *Baggage) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(bytes, b)
}

func DeserializeBaggage(s string) (Baggage, error) {
	result := Baggage{}
	// an encoded baggage is very much like a query string, so
	// make it look like one first and then parse it as such
	queryString := strings.ReplaceAll(s, ",", "&")
	values, err := url.ParseQuery(queryString)
	if err != nil {
		return Baggage{}, err
	}
	for k, v := range values {
		result[k] = v[0]
	}
	return result, nil
}
