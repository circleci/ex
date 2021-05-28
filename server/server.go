package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/o11y"
)

type Server struct {
	ctx      context.Context
	listener *trackedListener
	server   *http.Server
}

func NewServer(ctx context.Context, name, addr string, handler http.Handler) (s *Server, err error) {
	ctx, span := o11y.StartSpan(ctx, "server: new-server "+name)
	defer o11y.End(span, &err)
	span.AddField("server_name", name)
	span.AddField("address", addr)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	tr := &trackedListener{
		Listener: ln,
		name:     name,
	}
	ln = tr

	span.AddField("address", ln.Addr().String())

	return &Server{
		ctx:      ctx,
		listener: tr,
		server: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  55 * time.Second,
			WriteTimeout: 55 * time.Second,
		},
	}, nil
}

func (s *Server) Serve() error {
	g, ctx := errgroup.WithContext(s.ctx)

	g.Go(func() error {
		<-ctx.Done()
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(cctx)
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

func (s *Server) MetricsProducer() MetricProducer {
	return s.listener
}

func (s Server) Addr() string {
	return s.listener.Addr().String()
}
