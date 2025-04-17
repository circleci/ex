package o11ygin

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	hc "github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/o11y"
)

type requestVals struct {
	Req      *http.Request
	Route    string
	ClientIP string
}

func semconvServerRequest(span o11y.Span, v requestVals) {
	rURL := hc.RedactQueryString(*v.Req.URL)

	h, p := hc.HostPort(v.Req.Host)
	as := map[attribute.Key]any{
		semconv.ServerAddressKey:     h,
		semconv.ServerPortKey:        p,
		semconv.HTTPRequestMethodKey: v.Req.Method,
		semconv.URLPathKey:           rURL.Path,
		semconv.URLSchemeKey:         rURL.Scheme,
		semconv.HTTPRouteKey:         v.Route,
	}

	hc.SetString(as, semconv.URLQueryKey, rURL.RawQuery)
	hc.SetString(as, semconv.ClientAddressKey, v.ClientIP)
	hc.SetString(as, semconv.UserAgentOriginalKey, v.Req.Header.Get("User-Agent"))
	hc.SetString(as, "http.request.header.referer", v.Req.Header.Get("Referer"))

	for k, v := range as {
		span.AddRawField(string(k), v)
	}
}

func semconvServerResponse(span o11y.Span, status int) {
	span.AddRawField(string(semconv.HTTPResponseStatusCodeKey), status)
}
