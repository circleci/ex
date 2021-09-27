package httpserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/circleci/ex/system"
)

func Load(ctx context.Context, name, addr string, handler http.Handler, sys *system.System) (*HTTPServer, error) {
	server, err := New(ctx, name, addr, handler)
	if err != nil {
		return nil, fmt.Errorf("error starting %q server", name)
	}

	sys.AddService(server.Serve)
	sys.AddMetrics(server.MetricsProducer())
	return server, nil
}
