package testcontext

import (
	"context"

	"github.com/DataDog/datadog-go/statsd"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
)

// ctx is a global singleton, initialised at package time to avoid racy initiation of the global singleton
// inside deterministic_sampler.go
var ctx = newContext()

// Background returns a context for use in tests which contains a working o11y, so you get logs.
func Background() context.Context {
	return ctx
}

func newContext() context.Context {
	return o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		ServiceName: "test-service",
		Key:         "some-key",
		Format:      "color",
		Metrics:     &statsd.NoOpClient{},
	}))
}
