// Package o11ynethttp provides common http middleware for tracing requests.
package o11ynethttp

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/honeycombio/beeline-go/wrappers/common"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/baggage"
)

// Middleware returns an http.Handler which wraps an http.Handler and adds
// an o11y.Provider to the context. A new span is created from the request headers.
//
// This code is based on github.com/beeline-go/wrappers/hnynethttp/nethttp.go
func Middleware(provider o11y.Provider, name string, handler http.Handler) http.Handler {
	m := provider.MetricsProvider()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before := time.Now()

		// TODO: stop using honeycomb's built-in request parsing, use our own that follows otel naming specs
		ctx, span := common.StartSpanOrTraceFromHTTP(r)
		defer span.Send()

		ctx = o11y.WithProvider(ctx, provider)
		ctx = o11y.WithBaggage(ctx, baggage.Get(ctx, r))
		r = r.WithContext(ctx)

		provider.AddFieldToTrace(ctx, "server_name", name)
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

		if m != nil {
			_ = m.TimeInMilliseconds("handler",
				float64(time.Since(before).Nanoseconds())/1000000.0,
				[]string{
					"server_name:" + name,
					"request.method:" + r.Method,
					"request.route:unknown",
					"response.status_code:" + strconv.Itoa(sw.status),
					//TODO: "has_panicked:"+,
				},
				1,
			)
		}
	})
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
