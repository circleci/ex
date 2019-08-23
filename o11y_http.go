package o11y

import "net/http"

var httpWrapper HTTPWrapper

type HTTPWrapper interface {
	WrapHandlerFunc(hf func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request)
}

func WrapHandlerFunc(hf func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return httpWrapper.WrapHandlerFunc(hf)
}

func SetHttpWrapper(wrapper HTTPWrapper) {
	httpWrapper = wrapper
}
