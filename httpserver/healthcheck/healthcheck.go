// Package healthcheck implements the output admin API such as health checks and runtime profiling.
package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hellofresh/health-go/v4"

	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/system"
)

type API struct {
	router *gin.Engine
}

func New(ctx context.Context, checked []system.HealthChecker) (*API, error) {
	r := ginrouter.Default(ctx, "admin")

	heathLive, heathReady, err := newHealthHandlers(checked)
	if err != nil {
		return nil, fmt.Errorf("failed to create health checks: %w", err)
	}

	r.GET("/live", gin.WrapH(heathLive.Handler()))
	r.GET("/ready", gin.WrapH(heathReady.Handler()))

	debug := r.Group("/debug")
	debug.GET("/", gin.WrapF(pprof.Index))
	debug.GET("/cmdline/", gin.WrapF(pprof.Cmdline))
	debug.GET("/profile/", gin.WrapF(pprof.Profile))
	debug.GET("/symbol/", gin.WrapF(pprof.Symbol))
	debug.GET("/trace/", gin.WrapF(pprof.Trace))

	return &API{router: r}, nil
}

func (a *API) Handler() http.Handler {
	return a.router
}

func newHealthHandlers(checked []system.HealthChecker) (*health.Health, *health.Health, error) {
	heathLive, err := health.New()
	if err != nil {
		return nil, nil, err
	}

	heathReady, err := health.New()
	if err != nil {
		return nil, nil, err
	}

	for _, c := range checked {
		name, ready, live := c.HealthChecks()

		if ready != nil {
			err = heathReady.Register(health.Config{
				Name:      name,
				Timeout:   time.Second * 5,
				SkipOnErr: false,
				Check:     ready,
			})
			if err != nil {
				return nil, nil, err
			}
		}

		if live != nil {
			err = heathLive.Register(health.Config{
				Name:      name,
				Timeout:   time.Second * 5,
				SkipOnErr: false,
				Check:     live,
			})
			if err != nil {
				return nil, nil, err
			}
		}
	}

	return heathLive, heathReady, nil
}
