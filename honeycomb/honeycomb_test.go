package honeycomb

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/klauspost/compress/zstd"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/assert/opt"

	"github.com/circleci/distributor/o11y"
)

func TestHoneycomb(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"version":42`))
		assert.Check(t, cmp.Contains(event, `"name":"test-span"`))
		assert.Check(t, cmp.Contains(event, `"app.span-key":"span-value"`))
		assert.Check(t, cmp.Contains(event, `"app.trace-key":"trace-value"`))
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

	ctx, span := h.StartSpan(ctx, "test-span")
	h.AddFieldToTrace(ctx, "trace-key", "trace-value")
	span.AddField("span-key", "span-value")
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

	assert.Assert(t, gotEvent, "expected honeycomb to receive event")
}

func TestHoneycombMetrics(t *testing.T) {
	// set up a minimal no-op server
	gotEvent := false
	url := honeycombServer(t, func(e string) {
		gotEvent = true
		assert.Check(t, !strings.Contains(e, metricKey))
	})
	ctx := context.Background()

	fakeMetrics := &fakeMetrics{}
	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
		Metrics:    fakeMetrics,
	})
	h.AddGlobalField("version", 42)

	ctx, span := h.StartSpan(ctx, "test-span")
	span.RecordMetric(o11y.Timing("test-metric", "low-card-tag", "status.code"))
	span.AddField("low-card-tag", "tag-value")
	span.AddField("status.code", 500)
	span.AddField("another-tag", "another-value")
	span.End()
	h.Close(ctx)

	assert.Check(t, cmp.Len(fakeMetrics.calls, 1))
	assert.Check(t, cmp.DeepEqual(fakeMetrics.calls[0], metricCall{
		Metric: "timer",
		Name:   "test-metric",
		Tags:   []string{"low-card-tag:tag-value", "status.code:500"},
		Rate:   1,
	}, similarMetricValue))

	assert.Assert(t, gotEvent, "expected honeycomb to receive event")
}

func TestHoneycombWithError(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"version":123`))
		assert.Check(t, cmp.Contains(event, `"name":"test-span-with-error"`))
		assert.Check(t, cmp.Contains(event, `"app.span-key":"span-value-error"`))
		assert.Check(t, cmp.Contains(event, `"app.trace-key":"trace-value-error"`))
		assert.Check(t, cmp.Contains(event, `"app.result":"error"`))
		assert.Check(t, cmp.Contains(event, `"app.error":"example error"`))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "error-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version", 123)

	_ = func() (err error) {
		ctx, span := h.StartSpan(ctx, "test-span-with-error")
		defer o11y.End(span, &err)
		h.AddFieldToTrace(ctx, "trace-key", "trace-value-error")
		span.AddField("span-key", "span-value-error")
		return errors.New("example error")
	}()

	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
}

func TestHoneycombWithNilError(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"app.result":"success"`))
		assert.Check(t, not(cmp.Contains(event, `"app.error"`)))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "error-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version", 123)

	_, _ = func() (result string, err error) {
		_, span := h.StartSpan(ctx, "test-span-with-nil-error")
		defer o11y.End(span, &err)

		return "ok", nil
	}()

	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
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

var similarMetricValue = gocmp.FilterPath(
	opt.PathField(metricCall{}, "Value"),
	cmpopts.EquateApprox(0, 0.1),
)

type metricCall struct {
	Metric string
	Name   string
	Value  float64
	Tags   []string
	Rate   float64
}

type fakeMetrics struct {
	o11y.MetricsProvider
	calls []metricCall
}

func (f *fakeMetrics) TimeInMilliseconds(name string, value float64, tags []string, rate float64) error {
	f.calls = append(f.calls, metricCall{"timer", name, value, tags, rate})
	return nil
}

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
