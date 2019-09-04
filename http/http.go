package http

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/honeycombio/beeline-go/wrappers/common"

	"github.com/circleci/distributor/o11y"
)

// Middleware returns an http.Handler which wraps an http.Handler and adds
// an o11y.Provider to the context.
//
// A span is created from the request headers, or a new one is created if no
// request headers exist.
//
// This code is based on github.com/beeline-go/wrappers/hnynethttp/nethttp.go
func Middleware(provider o11y.Provider, name string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := common.StartSpanOrTraceFromHTTP(r)
		defer span.Send()

		ctx = o11y.WithProvider(ctx, provider)
		r = r.WithContext(ctx)

		provider.AddFieldToTrace(ctx, "server_name", name)
		// TODO: In future this should ideally be the route name, not the Path,
		//       but in order to do that, we'll need a standard routing
		//       abstraction that can be read when wrapped by more middleware
		span.AddField("name", fmt.Sprintf("%s %s %s", name, r.Method, r.URL.Path))

		sw := &statusWriter{ResponseWriter: w}
		handler.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = 200
		}
		span.AddField("response.status_code", sw.status)
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
