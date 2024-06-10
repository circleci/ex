/*
Package testcontextotel provides a context with o11y wired in, setup for coloured logging and no-op
metrics.

TL;DR - will give your tests pretty logs!
*/
package testcontextotel

import (
	"context"

	"github.com/DataDog/datadog-go/statsd"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
)

// ctx is a global singleton, initialised at package time to avoid racy initiation of the global singleton
// inside deterministic_sampler.go
var ctx = newContext()

// Background returns a context for use in tests which contains a working o11y, so you get logs.
func Background() context.Context {
	return ctx
}

func newContext() context.Context {
	o, err := otel.New(otel.Config{
		ResourceAttributes: []attribute.KeyValue{
			semconv.ServiceNameKey.String("test-service"),
			semconv.ServiceVersionKey.String("dev"),
			// Other Config specific fields
			attribute.String("service.mode", "mode"),
		},
		Metrics: &statsd.NoOpClient{},
	})
	if err != nil {
		return context.Background()
	}
	return o11y.WithProvider(context.Background(), o)
}
