package system

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/worker"
)

type MetricProducer interface {
	// MetricName The name for this group of metrics
	//(Name might be cleaner, but is much more likely to conflict in implementations)
	MetricName() string
	// Gauges are instantaneous name value pairs
	Gauges(context.Context) map[string]float64
}

func traceMetrics(ctx context.Context, producers []MetricProducer) {
	metrics := o11y.FromContext(ctx).MetricsProvider()
	for _, producer := range producers {
		traceMetric(ctx, metrics, producer)
	}
}

func traceMetric(ctx context.Context, provider o11y.MetricsProvider, producer MetricProducer) {
	producerName := strings.ReplaceAll(producer.MetricName(), "-", "_")
	for f, v := range producer.Gauges(ctx) {
		scopedField := fmt.Sprintf("gauge.%s.%s", producerName, f)
		_ = provider.Gauge(scopedField, v, []string{}, 1)
	}
}

// metrics reporter returns a function that is expected to be used in a call to errgroup.Go
// that func starts a worker that periodically calls and publishes the gauges from the producers.
func metricsReporter(ctx context.Context, mps []MetricProducer, gps []GaugeProducer) func() error {
	return func() error {
		cfg := worker.Config{
			Name:          "metric-loop",
			MaxWorkTime:   time.Second,
			NoWorkBackOff: backoff.NewConstantBackOff(time.Second * 10),
			WorkFunc: func(ctx context.Context) error {
				traceMetrics(ctx, mps)
				emitGauges(ctx, gps)
				return worker.ErrShouldBackoff
			},
		}
		worker.Run(ctx, cfg)
		return nil
	}
}
