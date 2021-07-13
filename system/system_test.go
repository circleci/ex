package system

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/termination"
	"github.com/circleci/ex/testing/testcontext"
)

func TestSystem_Run(t *testing.T) {
	ctx := testcontext.Background()

	// Wait until everything has been exercised before terminating
	terminationWait := &sync.WaitGroup{}
	terminationTestHook = func(ctx context.Context, delay time.Duration) error {
		terminationWait.Wait()
		return termination.ErrTerminated
	}

	sys := New(ctx)

	sys.AddMetrics(newMockMetricProducer(terminationWait))

	terminationWait.Add(1)
	sys.AddService(func(ctx context.Context) (err error) {
		ctx, span := o11y.StartSpan(ctx, "service")
		defer o11y.End(span, &err)
		terminationWait.Done()
		<-ctx.Done()
		return nil
	})

	sys.AddHealthCheck(newMockHealthChecker())

	cleanupCalled := false
	sys.AddCleanup(func(ctx context.Context) (err error) {
		ctx, span := o11y.StartSpan(ctx, "cleanup")
		defer o11y.End(span, &err)
		cleanupCalled = true
		return nil
	})

	err := sys.Run(0)
	assert.Check(t, errors.Is(err, termination.ErrTerminated))

	sys.Cleanup(ctx)
	assert.Check(t, cleanupCalled)
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

func (m *mockMetricProducer) Gauges(ctx context.Context) map[string]float64 {
	m.wg.Done()
	return map[string]float64{
		"key_a": 1,
		"key_b": 2,
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
