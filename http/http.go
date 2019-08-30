package http

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"

	"github.com/circleci/distributor/o11y"
	"github.com/honeycombio/beeline-go/wrappers/common"
)

func Middleware(rootCtx context.Context, handler http.Handler) http.Handler {
	// Cache handlerName and provider here for efficiency's sake
	provider := o11y.FromContext(rootCtx)
	handlerName := cleanHandlerName(runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name())

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		// make sure the provider is added to the request context
		ctx = o11y.WithProvider(ctx, provider)
		// set up our new per request root span

		if handlerName == "" {
			handlerName = "unknown"
		}

		ctx, span := o11y.StartSpan(ctx, fmt.Sprintf("%s %s", r.Method, handlerName))
		defer span.End()

		o11y.AddField(ctx, "handler", handlerName)

		for k, v := range common.GetRequestProps(r) {
			o11y.AddField(ctx, k, v)
		}

		// update the request with the new context
		r = r.WithContext(ctx)

		// inject our writer to capture the response
		sw := &statusWriter{
			ResponseWriter: w,
		}

		// serve the handler chain
		handler.ServeHTTP(sw, r)

		o11y.AddField(ctx, "response.status_code", sw.status)
	})
}

func cleanHandlerName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return name
	}
	return parts[len(parts)-1]
}

type statusWriter struct {
	http.ResponseWriter
	once   sync.Once
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	// only get the first set status
	w.once.Do(func() {
		w.status = status
		if w.status == 0 {
			w.status = http.StatusOK
		}
	})
	w.ResponseWriter.WriteHeader(status)
}
