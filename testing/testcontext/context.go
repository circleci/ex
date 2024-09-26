package testcontext

import (
	"context"

	"github.com/circleci/ex/config/o11y"
)

// ctx is a global singleton, initialised at package time to avoid racy initiation of the global singleton
// inside deterministic_sampler.go
var ctx = newContext()

// Background returns a context for use in tests which contains a working o11y, so you get logs.
func Background() context.Context {
	return ctx
}

func newContext() context.Context {
	cx, _, _ := o11y.Otel(context.Background(), o11y.OtelConfig{
		Service: "test-service",
		Test:    true,
	})
	return cx
}
