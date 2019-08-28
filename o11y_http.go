package o11y

import (
	"context"
	"net/http"
)

var httpWrapper HTTPWrapper

type HTTPWrapper interface {
	WrapHandlerFunc(hf func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request)
}

func WrapHandlerFunc(ctx context.Context, hf func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	h := func(w http.ResponseWriter, r *http.Request) {
		// inject our provider
		r = r.WithContext(WithProvider(r.Context(), FromContext(ctx)))
		hf(w, r)
	}
	return httpWrapper.WrapHandlerFunc(h)
}

func SetHttpWrapper(wrapper HTTPWrapper) {
	httpWrapper = wrapper
}
