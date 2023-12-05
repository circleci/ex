package otel

import (
	"fmt"
	"os"

	"github.com/DataDog/datadog-go/statsd"

	o11yconf "github.com/circleci/ex/config/o11y"
	"github.com/circleci/ex/o11y"
)

func metricsProvider(o o11yconf.Config) (o11y.ClosableMetricsProvider, error) {
	if o.Statsd == "" {
		return &statsd.NoOpClient{}, nil
	}

	hostname, _ := os.Hostname()

	tags := []string{
		"service:" + o.Service,
		"version:" + o.Version,
		"hostname:" + hostname,
	}
	if o.Mode != "" {
		tags = append(tags, "mode:"+o.Mode)
	}

	statsdOpts := []statsd.Option{
		statsd.WithNamespace(o.StatsNamespace),
		statsd.WithTags(tags),
	}
	if o.StatsdTelemetryDisabled {
		statsdOpts = append(statsdOpts, statsd.WithoutTelemetry())
	}

	stats, err := statsd.New(o.Statsd, statsdOpts...)
	if err != nil {
		return nil, fmt.Errorf("metrics provider failed: %w", err)
	}
	return stats, nil
}
