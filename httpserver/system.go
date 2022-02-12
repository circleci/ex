package httpserver

import (
	"context"
	"fmt"

	"github.com/circleci/ex/system"
)

func Load(ctx context.Context, cfg Config, sys *system.System) (*HTTPServer, error) {
	server, err := New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("error starting %q server", cfg.Name)
	}

	sys.AddService(server.Serve)
	sys.AddMetrics(server.MetricsProducer())
	return server, nil
}
