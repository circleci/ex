package testcontextotel

import (
	"context"

	o11yconf "github.com/circleci/ex/config/o11y"
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
	ctx := context.Background()
	o, err := otel.New(otel.Config{
		Config: o11yconf.Config{
			Service: "test-service",
			Mode:    "test",
			Version: "dev",
		},
	})
	if err != nil {
		return ctx
	}
	return o11y.WithProvider(ctx, o)
}
