package system

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/termination"
)

type System struct {
	group           *errgroup.Group
	ctx             context.Context
	services        []func(context.Context) error
	healthChecks    []HealthChecker
	metricProducers []MetricProducer
	cleanups        []func(ctx context.Context) error
}

func New(ctx context.Context) *System {
	group, ctx := errgroup.WithContext(ctx)
	return &System{
		group: group,
		ctx:   ctx,
	}
}

var terminationTestHook = termination.Handle

func (r *System) Run(delay time.Duration) (err error) {
	_, uptimeSpan := o11y.StartSpan(r.ctx, "system: run")
	defer o11y.End(uptimeSpan, &err)
	uptimeSpan.RecordMetric(o11y.Timing("system.run", "result"))

	r.group.Go(func() error {
		return terminationTestHook(r.ctx, delay)
	})

	for _, f := range r.services {
		// Capture the func, so we don't overwrite it when the goroutines start in parallel.
		f := f
		r.group.Go(func() error {
			return f(r.ctx)
		})
	}

	// if we have any metrics add the metrics worker
	if len(r.metricProducers) > 0 {
		r.group.Go(metricsReporter(r.ctx, r.metricProducers))
	}

	return r.group.Wait()
}

func (r *System) AddService(s func(ctx context.Context) error) {
	r.services = append(r.services, s)
}

func (r *System) AddHealthCheck(h HealthChecker) {
	r.healthChecks = append(r.healthChecks, h)
}

func (r *System) AddMetrics(m MetricProducer) {
	r.metricProducers = append(r.metricProducers, m)
}

func (r *System) AddCleanup(c func(ctx context.Context) error) {
	r.cleanups = append(r.cleanups, c)
}

func (r *System) HealthChecks() []HealthChecker {
	return r.healthChecks
}

func (r *System) Cleanup(ctx context.Context) {
	for _, c := range r.cleanups {
		err := c(ctx)
		if err != nil {
			o11y.Log(ctx, "system: cleanup error", o11y.Field("error", err))
		}
	}
}
