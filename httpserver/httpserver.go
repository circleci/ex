package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/o11y"
)

type HTTPServer struct {
	listener *trackedListener
	server   *http.Server
}

type Config struct {
	// Name is the name of the server in o11y
	Name string
	// Addr is the address to listen on
	Addr string
	// Handler is the  HTTP handler to delegate requests to.
	Handler http.Handler

	// Optional
	// Network must be "tcp", "tcp4", "tcp6", "unix", "unixpacket" or "" (which defaults to tcp).
	Network string
}

func New(ctx context.Context, cfg Config) (s *HTTPServer, err error) {
	ctx, span := o11y.StartSpan(ctx, "server: new-server "+cfg.Name)
	defer o11y.End(span, &err)
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}
	span.AddField("server_name", cfg.Name)
	span.AddField("address", cfg.Addr)
	span.AddField("network", cfg.Network)

	ln, err := net.Listen(cfg.Network, cfg.Addr)
	if err != nil {
		return nil, err
	}

	tr := &trackedListener{
		Listener: ln,
		name:     cfg.Name,
	}
	ln = tr

	span.AddField("address", ln.Addr().String())

	return &HTTPServer{
		listener: tr,
		server: &http.Server{
			Addr:         cfg.Addr,
			Handler:      cfg.Handler,
			ReadTimeout:  55 * time.Second,
			WriteTimeout: 55 * time.Second,
		},
	}, nil
}

// Serve the http server. On context cancellation the server is shutdown giving some time
// for the in flight requests to be handled.
func (s *HTTPServer) Serve(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-ctx.Done()
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.server.Shutdown(cctx); err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		err := s.server.Serve(s.listener)
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	return g.Wait()
}

func (s *HTTPServer) MetricsProducer() MetricProducer {
	return s.listener
}

func (s HTTPServer) Addr() string {
	return s.listener.Addr().String()
}
