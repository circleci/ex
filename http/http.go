package http

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/honeycombio/beeline-go/wrappers/common"

	"github.com/circleci/distributor/o11y"
)

func Middleware(rootCtx context.Context, name string, handler http.Handler) http.Handler {
	// This code is based on github.com/beeline-go/wrappers/hnynethttp/nethttp.go
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := common.StartSpanOrTraceFromHTTP(r)
		defer span.Send()

		// make sure our provider is added to the request context
		// otherwise the o11y functions wont work inside handlers
		ctx = o11y.CopyProvider(rootCtx, ctx)
		r = r.WithContext(ctx)

		o11y.AddFieldToTrace(ctx, "server_name", name)
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
