package honeycomb

import (
	"net/http"

	"github.com/honeycombio/beeline-go/wrappers/hnynethttp"
)

type honeycombHTTPWrapper struct{}

func (h honeycombHTTPWrapper) WrapHandlerFunc(hf func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return hnynethttp.WrapHandlerFunc(hf)
}

func NewWrapper() *honeycombHTTPWrapper {
	return &honeycombHTTPWrapper{}
}
