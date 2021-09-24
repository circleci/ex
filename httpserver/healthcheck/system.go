package healthcheck

import (
	"context"
	"fmt"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/system"
)

func Load(ctx context.Context, addr string, sys *system.System) error {
	healthAPI, err := New(ctx, sys.HealthChecks())
	if err != nil {
		return fmt.Errorf("error creating health check API")
	}

	return httpserver.Load(ctx, "admin", addr, healthAPI.Handler(), sys)
}
