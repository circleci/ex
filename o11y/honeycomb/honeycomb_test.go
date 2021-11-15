package honeycomb

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/klauspost/compress/zstd"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/fakemetrics"
)

func TestHoneycomb(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"version":42`))
		assert.Check(t, cmp.Contains(event, `"name":"test-span"`))
		assert.Check(t, cmp.Contains(event, `"app.span_key":"span-value"`), "span.AddField is prefixed")
		assert.Check(t, cmp.Contains(event, `"raw_key":"span-value"`), "span.AddRawField is unprefixed")
		assert.Check(t, cmp.Contains(event, `"app.another_key":"span-value"`), "o11y.AddField is prefixed")
		assert.Check(t, cmp.Contains(event, `"app.trace_key":"trace-value"`), "o11y.AddFieldToTrace is prefixed")
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
	})

	h.AddGlobalField("version", 42)

	ctx = o11y.WithProvider(ctx, h)
	ctx, span := o11y.StartSpan(ctx, "test-span")
	o11y.AddFieldToTrace(ctx, "trace_key", "trace-value")
	o11y.AddField(ctx, "another_key", "span-value")
	span.AddField("span_key", "span-value")
	span.AddRawField("raw_key", "span-value")
	span.End()
	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
}

func TestHoneycomb_NormalizeKeys(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"name":"test-span"`))

		assert.Check(t, cmp.Contains(event, `"version_number":42`))
		assert.Check(t, cmp.Contains(event, `"app.trace_key":"trace-value"`))
		assert.Check(t, cmp.Contains(event, `"app.another_key":"another-value"`))

		assert.Check(t, cmp.Contains(event, `"app.span_key":"span-value"`))
		assert.Check(t, cmp.Contains(event, `"raw_key":"raw-value"`))
	}

	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version-number", 42)

	ctx = o11y.WithProvider(ctx, h)
	ctx, span := o11y.StartSpan(ctx, "test-span")

	o11y.AddFieldToTrace(ctx, "trace-key", "trace-value")
	o11y.AddField(ctx, "another-key", "another-value")
	span.AddField("span-key", "span-value")
	span.AddRawField("raw-key", "raw-value")

	span.End()
	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
}

func TestHoneycombMetricsDoesntPolluteWhenNotConfigured(t *testing.T) {
	// For horrible constructor-masking-a-singleton reasons this needed to run
	// before any test which enables metrics
	// I could probably have fixed this with a bunch of yak shaving, but it didn't seem worth it
	// In any case, the fix to make this test pass actually resolves the ordering issue
	// but if there's a regression, its likely that order will be important again :(

	gotEvent := false
	url := honeycombServer(t, func(e string) {
		gotEvent = true
		assert.Check(t, !strings.Contains(e, metricKey))
	})
	ctx := context.Background()

	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version", 42)

	ctx, span := h.StartSpan(ctx, "test-span")
	span.RecordMetric(o11y.Timing("test-metric"))
	span.End()
	h.Close(ctx)

	assert.Check(t, gotEvent, "expected honeycomb to receive event")
}

func TestHoneycombMetrics(t *testing.T) {
	// set up a minimal no-op server
	gotEvent := false
	url := honeycombServer(t, func(e string) {
		gotEvent = true
		assert.Check(t, !strings.Contains(e, metricKey))
	})
	ctx := context.Background()

	fakeMetrics := &fakemetrics.Provider{}
	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
		Metrics:    fakeMetrics,
	})
	h.AddGlobalField("version", 42)

	ctx, span := h.StartSpan(ctx, "test-span")
	span.RecordMetric(o11y.Timing("test-metric-timing", "low_card_tag", "status.code"))
	span.RecordMetric(o11y.Incr("test-metric-incr", "low_card_tag", "status.code"))
	span.RecordMetric(o11y.Duration("test-duration-ms", "latency", "status.code"))
	span.AddField("low_card_tag", "tag-value")
	span.AddField("status.code", 500)
	span.AddField("another_tag", "another-value")
	span.AddField("latency", time.Second)

	span.AddField("to_gauge", 122.87)
	span.RecordMetric(o11y.Gauge("test_metric_gauge", "to_gauge"))
	span.AddField("to_count", 134)
	span.AddField("to_count_2", 145)
	span.RecordMetric(o11y.Count("test_metric_count", "to_count", o11y.NewTag("type", "first")))
	span.RecordMetric(o11y.Count("test_metric_count", "to_count_2", o11y.NewTag("type", "second")))
	span.End()
	h.Close(ctx)

	calls := fakeMetrics.Calls()
	assert.Assert(t, cmp.Len(fakeMetrics.Calls(), 6))
	assert.Check(t, cmp.DeepEqual(calls[0], fakemetrics.MetricCall{
		Metric: "timer",
		Name:   "test-metric-timing",
		Tags:   []string{"low_card_tag:tag-value", "status.code:500"},
		Rate:   1,
		Value:  10,
	}, cmpNonZeroValue))

	assert.Check(t, cmp.DeepEqual(calls[1], fakemetrics.MetricCall{
		Metric:   "count",
		Name:     "test-metric-incr",
		Tags:     []string{"low_card_tag:tag-value", "status.code:500"},
		Rate:     1,
		ValueInt: 1,
	}))

	assert.Check(t, cmp.DeepEqual(calls[2], fakemetrics.MetricCall{
		Metric: "timer",
		Name:   "test-duration-ms",
		Tags:   []string{"status.code:500"},
		Rate:   1,
		Value:  1000,
	}))

	assert.Check(t, cmp.DeepEqual(calls[3], fakemetrics.MetricCall{
		Metric: "gauge",
		Name:   "test_metric_gauge",
		Tags:   []string{},
		Rate:   1,
		Value:  122.87,
	}))

	assert.Check(t, cmp.DeepEqual(calls[4], fakemetrics.MetricCall{
		Metric:   "count",
		Name:     "test_metric_count",
		Tags:     []string{"type:first"},
		Rate:     1,
		ValueInt: 134,
	}))

	assert.Check(t, cmp.DeepEqual(calls[5], fakemetrics.MetricCall{
		Metric:   "count",
		Name:     "test_metric_count",
		Tags:     []string{"type:second"},
		Rate:     1,
		ValueInt: 145,
	}))

	assert.Check(t, gotEvent, "expected honeycomb to receive event")
}

func TestHoneycombWithError(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"name":"test-span-with-error"`))
		assert.Check(t, cmp.Contains(event, `"result":"error"`))
		assert.Check(t, cmp.Contains(event, `"error":"example error"`))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "error-dataset",
		Host:       url,
		SendTraces: true,
	})

	_ = func() (err error) {
		_, span := h.StartSpan(ctx, "test-span-with-error")
		defer o11y.End(span, &err)
		return errors.New("example error")
	}()

	h.Close(ctx)

	assert.Check(t, gotEvent, "expected to receive an event")
}

func TestHoneycombWithNilError(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"result":"success"`))
		assert.Check(t, not(cmp.Contains(event, `"error"`)))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "error-dataset",
		Host:       url,
		SendTraces: true,
	})

	_, _ = func() (result string, err error) {
		_, span := h.StartSpan(ctx, "test-span-with-nil-error")
		defer o11y.End(span, &err)

		return "ok", nil
	}()

	h.Close(ctx)

	assert.Check(t, gotEvent, "expected to receive an event")
}

func honeycombServer(t *testing.T, cb func(string)) string {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := zstd.NewReader(r.Body)
		if err != nil {
			t.Fatal("could not create zip reader", err)
		}
		defer reader.Close()
		defer r.Body.Close()

		b, err := ioutil.ReadAll(reader)
		if err != nil {
			t.Error("could not read request", err)
		}
		cb(string(b))
	}))
	return ts.URL
}

var cmpNonZeroValue = gocmp.Options{gocmp.Comparer(func(a, b float64) bool {
	return a > 0 && b > 0
})}

func not(c cmp.Comparison) cmp.Comparison {
	return func() cmp.Result {
		return InvertedResult{c()}
	}
}

type InvertedResult struct {
	cmp.Result
}

func (r InvertedResult) Success() bool {
	return !r.Result.Success()
}
