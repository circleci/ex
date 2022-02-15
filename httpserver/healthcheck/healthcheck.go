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

	r.GET("/debug/pprof/*prof", handlePprof)

	return &API{router: r}, nil
}

func handlePprof(c *gin.Context) {
	// There are special profiles that are not served from the generic pprof
	// index handler.
	// The index handler expects a debug/pprof prefix, and because of
	// Gins wildcard handling, and the desire to serve these on the
	// same paths as the std lib (keeping the index links accurate)
	// we need to handle the 4 special profiles specifically.
	switch c.Param("prof") {
	case "/cmdline":
		gin.WrapF(pprof.Cmdline)(c)
	case "/profile":
		gin.WrapF(pprof.Profile)(c)
	case "/symbol":
		gin.WrapF(pprof.Symbol)(c)
	case "/trace":
		gin.WrapF(pprof.Trace)(c)
	default:
		gin.WrapF(pprof.Index)(c)
	}
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
