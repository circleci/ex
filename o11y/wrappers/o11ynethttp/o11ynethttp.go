// Package o11ynethttp provides common http middleware for tracing requests.
package o11ynethttp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/honeycombio/beeline-go/wrappers/common"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/baggage"
)

type nethttpRouteRecorderContextKey struct{}

// Middleware returns an http.Handler which wraps an http.Handler and adds
// an o11y.Provider to the context. A new span is created from the request headers.
//
// This code is based on github.com/beeline-go/wrappers/hnynethttp/nethttp.go
func Middleware(provider o11y.Provider, name string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before := time.Now()

		// TODO: stop using honeycomb's built-in request parsing, use our own that follows otel naming specs
		ctx, span := common.StartSpanOrTraceFromHTTP(r)
		defer span.Send()

		provider.AddFieldToTrace(ctx, "server_name", name)
		routeRecorder := NewRouteRecorder()
		ctx = o11y.WithProvider(ctx, provider)
		ctx = o11y.WithBaggage(ctx, baggage.Get(ctx, r))
		ctx = context.WithValue(ctx, nethttpRouteRecorderContextKey{}, routeRecorder)
		r = r.WithContext(ctx)

		// We default to using the Path as the name and route - which could be high cardinality
		// We expect consumers to override these fields if they have something better
		span.AddField("name", fmt.Sprintf("http-server %s: %s %s", name, r.Method, r.URL.Path))
		span.AddField("request.route", "unknown")

		sw := &statusWriter{ResponseWriter: w}
		handler.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = 200
		}
		span.AddField("response.status_code", sw.status)

		m := provider.MetricsProvider()
		if m != nil {
			_ = m.TimeInMilliseconds("handler",
				float64(time.Since(before).Nanoseconds())/1000000.0,
				[]string{
					"server_name:" + name,
					"request.method:" + r.Method,
					"request.route:" + routeRecorder.Route(),
					"response.status_code:" + strconv.Itoa(sw.status),
					//TODO: "has_panicked:"+,
				},
				1,
			)
		}
	})
}

type RouteRecorder struct {
	route string
	mu    sync.RWMutex
}

func NewRouteRecorder() *RouteRecorder {
	return &RouteRecorder{
		route: "unknown",
		mu:    sync.RWMutex{},
	}
}

func (r *RouteRecorder) SetRoute(route string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.route = route
}

func (r *RouteRecorder) Route() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.route
}

func GetRouteRecorderFromContext(ctx context.Context) *RouteRecorder {
	if ctx != nil {
		if val := ctx.Value(nethttpRouteRecorderContextKey{}); val != nil {
			if span, ok := val.(*RouteRecorder); ok {
				return span
			}
		}
	}
	return nil
}

type statusWriter struct {
	http.ResponseWriter
	once   sync.Once
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.once.Do(func() {
		w.status = status
	})
	w.ResponseWriter.WriteHeader(status)
}
