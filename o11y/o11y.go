// Package o11y provides observability in the form of tracing and metrics
package o11y

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/rollbar/rollbar-go"
	"go.opentelemetry.io/otel/baggage"
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
	// Opts are generally expected to be set by other ex packages, rather than application code
	StartSpan(ctx context.Context, name string, opts ...SpanOpt) (context.Context, Span)

	// GetSpan returns the active span in the given context. It will return nil if there is no span available.
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

	// Helpers returns some specific helper functions. Temporary optional param during the cutover to otel
	Helpers(disableW3c ...bool) Helpers

	// MakeSpanGolden Add a golden span from the span currently in the context.
	// If the golden trace does not exist it will be started.
	MakeSpanGolden(ctx context.Context) context.Context
}

// PropagationContext contains trace context values that are propagated from service to service.
// Typically, the Parent field is also present as a value in the Headers map.
// This seeming DRY breakage is so the special value for the field name of the Parent trace ID is not
// leaked, and accidentally depended upon.
type PropagationContext struct {
	// Parent contains single string serialisation of just the trace parent fields
	Parent string
	// Headers contains the map of all context propagation headers
	Headers http.Header
}

// PropagationContextFromHeader is a helper constructs a PropagationContext from h. It is not filtered
// to the headers needed for propagation. It is expected to be used as the input to InjectPropagation.
func PropagationContextFromHeader(h http.Header) PropagationContext {
	return PropagationContext{
		Headers: h,
	}
}

type Helpers interface {
	// ExtractPropagation pulls propagation information out of the context
	ExtractPropagation(ctx context.Context) PropagationContext
	// InjectPropagation adds propagation header fields into the returned root span returning
	// the context carrying that span
	InjectPropagation(context.Context, PropagationContext, ...SpanOpt) (context.Context, Span)

	// TraceIDs return standard o11y ids - used for testing
	TraceIDs(ctx context.Context) (traceID, parentID string)
	// GoldenTraceID returns the golden trace id - for testing
	//GoldenTraceID(ctx context.Context) string
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

	// Flatten causes all child span attributes to be set on this span, with the given prefix
	Flatten(prefix string)

	// End sets the duration of the span and tells the related provider that the span is complete,
	// so it can do its appropriate processing. The span should not be used after End is called.
	End()
}

type MetricType string

const (
	MetricTimer = "timer"
	MetricGauge = "gauge"
	MetricCount = "count"
)

type Metric struct {
	Type MetricType
	// Name is the metric name that will be emitted
	Name string
	// Field is the span field to use as the metric's value
	Field string
	// FixedTag is an optional tag added at Metric definition time
	FixedTag *Tag
	// TagFields are additional span fields to use as metric tags
	TagFields []string
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
	// Histogram aggregates values agent side for a period of time.
	// This is similar to TimeInMilliseconds, but not limited to timing data
	Histogram(name string, value float64, tags []string, rate float64) error
	// TimeInMilliseconds measures timing data only. For example, how long a network call takes
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

// FromContext returns the provider stored in the context, or the default noop
// provider if none exists.
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
func StartSpan(ctx context.Context, name string, opts ...SpanOpt) (context.Context, Span) {
	return FromContext(ctx).StartSpan(ctx, name, opts...)
}

// AddField adds a field to the currently active span
func AddField(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddField(ctx, key, val)
}

// AddFieldToTrace adds a field to the currently active root span and all of its current and future child spans
func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddFieldToTrace(ctx, key, val)
}

// MakeSpanGolden Add a golden span from the span currently in the context.
// If the golden trace does not exist it will be started.
func MakeSpanGolden(ctx context.Context) context.Context {
	return FromContext(ctx).MakeSpanGolden(ctx)
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
		if isInternalBaggage(k) {
			continue
		}
		k := strings.ReplaceAll(k, "-", "_")
		o.AddFieldToTrace(ctx, k, v)
	}
}

type baggageKey struct{}

func WithBaggage(ctx context.Context, b Baggage) context.Context {
	bg := baggage.FromContext(ctx)
	for k, v := range b {
		m, err := baggage.NewMemberRaw(k, v)
		if err != nil {
			AddField(ctx, "baggage_error", err)
			continue
		}
		bg, err = bg.SetMember(m)
		if err != nil {
			AddField(ctx, "baggage_error", err)
		}
	}
	ctx = baggage.ContextWithBaggage(ctx, bg)
	b.addToTrace(ctx)
	return context.WithValue(ctx, baggageKey{}, b)
}

func GetBaggage(ctx context.Context) Baggage {
	rbg := Baggage{}
	bg := baggage.FromContext(ctx)
	for _, m := range bg.Members() {
		rbg[m.Key()] = m.Value()
		// N.B. baggage properties not supported
	}

	b, ok := ctx.Value(baggageKey{}).(Baggage)
	if !ok {
		return rbg
	}
	for k, v := range b {
		rbg[k] = v
	}
	return rbg
}

var defaultProvider = &noopProvider{}

type noopProvider struct{}

func (c *noopProvider) AddGlobalField(string, interface{}) {}

func (c *noopProvider) StartSpan(ctx context.Context, _ string, _ ...SpanOpt) (context.Context, Span) {
	return ctx, &noopSpan{}
}

func (c *noopProvider) MakeSpanGolden(ctx context.Context) context.Context { return ctx }

func (c *noopProvider) GetSpan(context.Context) Span {
	return &noopSpan{}
}

func (c *noopProvider) AddField(context.Context, string, interface{}) {}

func (c *noopProvider) AddFieldToTrace(context.Context, string, interface{}) {}

func (c *noopProvider) Close(context.Context) {}

func (c *noopProvider) Log(context.Context, string, ...Pair) {}

func (c *noopProvider) MetricsProvider() MetricsProvider {
	return &statsd.NoOpClient{}
}

func (c *noopProvider) Helpers(...bool) Helpers {
	return noopHelpers{}
}

type noopHelpers struct{}

func (n noopHelpers) ExtractPropagation(_ context.Context) PropagationContext {
	return PropagationContext{}
}

func (n noopHelpers) InjectPropagation(ctx context.Context,
	_ PropagationContext, _ ...SpanOpt) (context.Context, Span) {

	return ctx, &noopSpan{}
}

func (n noopHelpers) TraceIDs(_ context.Context) (traceID, parentID string) {
	return "", ""
}

type noopSpan struct{}

func (s *noopSpan) AddField(key string, val interface{})    {}
func (s *noopSpan) AddRawField(key string, val interface{}) {}
func (s *noopSpan) RecordMetric(metric Metric)              {}
func (s *noopSpan) End()                                    {}
func (s *noopSpan) Flatten(string)                          {}

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

const flattenDepthBaggageKey = "flatten"

func ExtrasFromBaggage(ctx context.Context) (flatten int, gold http.Header) {
	b := GetBaggage(ctx)
	depth := 0
	if v, ok := b[flattenDepthBaggageKey]; ok {
		depth, _ = strconv.Atoi(v)
	}
	gold = http.Header{}
	for k, v := range b {
		gk := strings.TrimPrefix(k, goldenTracePrefix)
		if gk != k {
			gold.Add(gk, v)
		}
	}
	return depth, gold
}

const goldenTracePrefix = "golden-"

func AddExtrasToBaggage(ctx context.Context, flatten int, gold http.Header) context.Context {
	bag := Baggage{}
	if flatten > 0 {
		bag[flattenDepthBaggageKey] = strconv.Itoa(flatten)
	}
	for k, v := range gold {
		if len(v) > 0 {
			bag[goldenTracePrefix+k] = v[0]
		}
	}
	if len(bag) == 0 {
		return ctx
	}
	return WithBaggage(ctx, bag)
}

func isInternalBaggage(key string) bool {
	if key == flattenDepthBaggageKey {
		return true
	}
	if strings.HasPrefix(key, goldenTracePrefix) {
		return true
	}
	return false
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

// Propagation extracts a trace propagation string from the context.
func Propagation(ctx context.Context) string {
	pc := FromContext(ctx).Helpers().ExtractPropagation(ctx)
	b, err := json.Marshal(pc.Headers)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// SpanFromPropagation starts a span in the trace context injected from the propagation string.
func SpanFromPropagation(ctx context.Context, name, propagation string) (context.Context, Span) {
	pr := PropagationContext{}
	// We are ok to fail silently with no propagation
	b, _ := base64.StdEncoding.DecodeString(propagation)
	_ = json.Unmarshal(b, &pr.Headers)
	ctx, span := FromContext(ctx).Helpers().InjectPropagation(ctx, pr)
	span.AddRawField("name", name)
	return ctx, span
}
