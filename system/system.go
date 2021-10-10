package system

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/termination"
)

type HealthChecker interface {
	HealthChecks() (name string, ready, live func(ctx context.Context) error)
}

// System is a list of concurrent services that provides useful features
// for those services, such as coordinated cancellation and service metrics.
// It can collect a set of health check functions and return them as a list
// (to pass into single health check handler for instance).
type System struct {
	services        []func(context.Context) error
	healthChecks    []HealthChecker
	metricProducers []MetricProducer
	cleanups        []func(ctx context.Context) error
}

// New create a new system with a context that can be used to coordinate
// cancellation of the added services. The context is also expected to contain
// an o11y provider that will be used to produce metrics.
// It is expected that the context cancelled when the service receives a signal,
// but will also be cancelled when any of the services returns an error.
// The context may be cancelled by the caller, to stop the services.
func New() *System {
	return &System{}
}

// The default handler will return an error when a termination signal is received
// This variable is defined purely for internal testing
var terminationTestHook = termination.Handle

// Run runs any services added to the system, it also adds a signal handler that
// a worker to gather and publish system metrics.
// Run is blocking and will only return when all it's services have finished.
// The error returned will be the first error returned from any of the services.
// The terminationDelay passed in is the amount of time to wait between receiving a
// signal and cancelling the system context
func (r *System) Run(ctx context.Context, terminationDelay time.Duration) (err error) {
	_, uptimeSpan := o11y.StartSpan(ctx, "system: run")
	defer o11y.End(uptimeSpan, &err)
	uptimeSpan.RecordMetric(o11y.Timing("system.run", "result"))

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return terminationTestHook(ctx, terminationDelay)
	})

	for _, f := range r.services {
		// Capture the func, so we don't overwrite it when the goroutines start in parallel.
		f := f
		g.Go(func() error {
			return f(ctx)
		})
	}

	// if we have any metrics add the metrics worker
	if len(r.metricProducers) > 0 {
		g.Go(metricsReporter(ctx, r.metricProducers))
	}

	return g.Wait()
}

// AddService adds the service function to the list of coordinated services.
// Once the system Run is called each service function will be invoked.
// The context passed into each service can be used to coordinate graceful shutdown.
// Each service should monitor the context for cancellation then stop taking on new work,
// and allow in flight work to complete (often called 'draining') before returning.
// It is expected that services that need to do any final work to exit gracefully will
// have added a cleanup function.
// If a service depends on other services or utilities (such as a database connection) to complete
// in-flight work then the depended upon systems should remain active enough during a context
// cancellation, and only full shut down via a cleanup function (for instance closing a database connection).
func (r *System) AddService(s func(ctx context.Context) error) {
	r.services = append(r.services, s)
}

// AddHealthCheck stores a health checker for later retrieval. It is generally a good idea
// for each service added to also add a health checker to represent the liveness and readiness
// of the service, though some services will be simple enough that a health checker is not needed.
func (r *System) AddHealthCheck(h HealthChecker) {
	r.healthChecks = append(r.healthChecks, h)
}

// AddMetrics adds a metrics producer the the list of producers. These producers
// will be called periodically and the resultant gauges published via the system context.
func (r *System) AddMetrics(m MetricProducer) {
	r.metricProducers = append(r.metricProducers, m)
}

// AddCleanup stores function in the system that will be called when Cleanup is called.
// The functions added here will be invoked when Cleanup is called, which is typically.
// after Run has returned.
func (r *System) AddCleanup(c func(ctx context.Context) error) {
	r.cleanups = append(r.cleanups, c)
}

// HealthChecks returns the list of previously stored health checkers. This list can
// be used to report on the liveness and readiness of the system.
func (r *System) HealthChecks() []HealthChecker {
	return r.healthChecks
}

// Cleanup calls each function previously added with AddCleanup. It is expected to
// be called after Run has returned to do any final work to allow the system to exit gracefully.
func (r *System) Cleanup(ctx context.Context) {
	for _, c := range r.cleanups {
		err := c(ctx)
		if err != nil {
			o11y.Log(ctx, "system: cleanup error", o11y.Field("error", err))
		}
	}
}
