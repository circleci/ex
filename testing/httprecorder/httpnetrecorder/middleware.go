/*
Package httpnetrecorder provides a middleware to wire a httprecorder into test fakes.
*/
package httpnetrecorder

import (
	"context"
	"net/http"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/httprecorder"
)

func Middleware(ctx context.Context, rec *httprecorder.RequestRecorder, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := rec.Record(r)
		if err != nil {
			o11y.LogError(ctx, "problem recording HTTP request", err)
		}
		h.ServeHTTP(w, r)
	})
}
