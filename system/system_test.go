package system

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/termination"
	"github.com/circleci/ex/testing/fakemetrics"
)

func TestSystem_Run(t *testing.T) {
	metrics := &fakemetrics.Provider{}
	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: metrics,
	}))

	// Wait until everything has been exercised before terminating
	terminationWait := &sync.WaitGroup{}
	terminationTestHook = func(ctx context.Context, delay time.Duration) error {
		terminationWait.Wait()
		return termination.ErrTerminated
	}

	sys := New()

	sys.AddMetrics(newMockMetricProducer(terminationWait))
	sys.AddGauges(newMockGaugeProducer(terminationWait))

	terminationWait.Add(1)
	sys.AddService(func(ctx context.Context) (err error) {
		ctx, span := o11y.StartSpan(ctx, "service")
		defer o11y.End(span, &err)
		terminationWait.Done()
		<-ctx.Done()
		return nil
	})

	sys.AddHealthCheck(newMockHealthChecker())

	var cleanupsCalled []string
	sys.AddCleanup(func(ctx context.Context) (err error) {
		ctx, span := o11y.StartSpan(ctx, "cleanup 1")
		defer o11y.End(span, &err)
		cleanupsCalled = append(cleanupsCalled, "1")
		return nil
	})
	sys.AddCleanup(func(ctx context.Context) (err error) {
		ctx, span := o11y.StartSpan(ctx, "cleanup 2")
		defer o11y.End(span, &err)
		cleanupsCalled = append(cleanupsCalled, "2")
		return nil
	})

	err := sys.Run(ctx, 0)
	assert.Check(t, errors.Is(err, termination.ErrTerminated))

	sys.Cleanup(ctx)
	assert.Check(t, cmp.DeepEqual([]string{"2", "1"}, cleanupsCalled))

	assert.Check(t, cmp.DeepEqual(metrics.Calls(), []fakemetrics.MetricCall{
		{
			Metric: "gauge",
			Name:   "gauge..key_a",
			Value:  1,
			Tags:   []string{},
			Rate:   1,
		},
		{
			Metric: "gauge",
			Name:   "gauge..key_b",
			Value:  2,
			Tags:   []string{},
			Rate:   1,
		},
		{
			Metric: "gauge",
			Name:   "gauge.gauge_producer.key_a",
			Value:  1,
			Tags:   []string{"foo:bar"},
			Rate:   1,
		},
		{
			Metric: "gauge",
			Name:   "gauge.gauge_producer.key_b",
			Value:  2,
			Tags:   []string{"baz:qux"},
			Rate:   1,
		},
		{
			Metric: "timer",
			Name:   "worker_loop",
			Value:  0.01,
			Tags:   []string{"loop_name:metric-loop", "result:success"},
			Rate:   1,
		},
		{
			Metric: "timer",
			Name:   "system.run",
			Value:  0.3,
			Tags:   []string{"result:success"},
			Rate:   1,
		},
	}, cmpMetrics))
}

var cmpMetrics = gocmp.Options{
	cmpopts.IgnoreFields(fakemetrics.MetricCall{}, "Value"),
	cmpopts.SortSlices(func(x, y fakemetrics.MetricCall) bool {
		const format = "%s|%s|%s"
		return fmt.Sprintf(format, x.Metric, x.Name, x.Tags) <
			fmt.Sprintf(format, y.Metric, y.Name, y.Tags)
	}),
}

type mockMetricProducer struct {
	wg *sync.WaitGroup
}

func newMockMetricProducer(wg *sync.WaitGroup) *mockMetricProducer {
	wg.Add(2)
	return &mockMetricProducer{wg: wg}
}

func (m *mockMetricProducer) MetricName() string {
	m.wg.Done()
	return ""
}

func (m *mockMetricProducer) Gauges(_ context.Context) map[string]float64 {
	m.wg.Done()
	return map[string]float64{
		"key_a": 1,
		"key_b": 2,
	}
}

type mockGaugeProducer struct {
	wg *sync.WaitGroup
}

func newMockGaugeProducer(wg *sync.WaitGroup) *mockGaugeProducer {
	wg.Add(2)
	return &mockGaugeProducer{wg: wg}
}

func (m *mockGaugeProducer) GaugeName() string {
	m.wg.Done()
	return "gauge_producer"
}

func (m *mockGaugeProducer) Gauges(_ context.Context) map[string][]TaggedValue {
	m.wg.Done()
	return map[string][]TaggedValue{
		"key_a": {{Val: 1, Tags: []string{"foo:bar"}}},
		"key_b": {{Val: 2, Tags: []string{"baz:qux"}}},
	}
}

type mockHealthChecker struct {
}

func newMockHealthChecker() *mockHealthChecker {
	return &mockHealthChecker{}
}

func (m *mockHealthChecker) HealthChecks() (name string, ready, live func(ctx context.Context) error) {
	return "name", nil, nil
}
