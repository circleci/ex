package httpclient

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	"github.com/circleci/ex/o11y"
)

func SetString(a map[attribute.Key]any, k attribute.Key, v string) {
	if v == "" {
		return
	}
	a[k] = v
}

func RedactQueryString(u url.URL) url.URL {
	q := u.Query()
	if q.Has("circle-token") {
		q.Set("circle-token", "REDACTED")
	}
	u.RawQuery = q.Encode()
	return u
}

func HostPort(in string) (host, port string) {
	host, port, err := net.SplitHostPort(in)
	if err != nil {
		ae := &net.AddrError{}
		if errors.As(err, &ae) {
			if ae.Err == "missing port in address" {
				return in, ""
			}
		}
		return "unknown", ""
	}
	return host, port
}

func addSemconvResponseAttrs(span o11y.Span, res *http.Response) {
	as := map[attribute.Key]any{
		semconv.HTTPResponseStatusCodeKey: res.StatusCode,
	}
	if res.StatusCode >= http.StatusBadRequest {
		as[semconv.ErrorTypeKey] = strconv.Itoa(res.StatusCode)
	}

	SetString(as, semconv.HTTPResponseBodySizeKey, res.Header.Get("Content-Length"))
	SetString(as, "http.response.header.content-type", res.Header.Get("Content-Type"))
	SetString(as, "http.response.header.content-encoding", res.Header.Get("Content-Encoding"))
	SetString(as, "http.response.header.x-ratelimit-limit", res.Header.Get("x-ratelimit-limit"))
	SetString(as, "http.response.header.x-ratelimit-remaining", res.Header.Get("x-ratelimit-remaining"))
	SetString(as, "http.response.header.x-ratelimit-reset", res.Header.Get("x-ratelimit-reset"))
	SetString(as, "http.response.header.x-ratelimit-used", res.Header.Get("x-ratelimit-used"))

	for k, v := range as {
		span.AddRawField(string(k), v)
	}
}

type requestVals struct {
	Req        *http.Request
	Route      string
	Attempt    int
	ClientName string
}

func addSemconvRequestAttrs(span o11y.Span, v requestVals) {
	u := cleanURL(v.Req)

	h, p := HostPort(v.Req.Host)

	for k, v := range map[attribute.Key]any{
		semconv.ServerAddressKey:          h,
		semconv.ServerPortKey:             p,
		"backplane.client.name":           v.ClientName,
		semconv.URLTemplateKey:            v.Route,
		semconv.HTTPRequestMethodKey:      v.Req.Method,
		semconv.URLFullKey:                u.String(),
		semconv.HTTPRequestResendCountKey: v.Attempt,
		semconv.URLSchemeKey:              u.Scheme,
		semconv.UserAgentOriginalKey:      v.Req.UserAgent(),
		semconv.HTTPRequestBodySizeKey:    v.Req.ContentLength,
	} {
		span.AddRawField(string(k), v)
	}
}

// cleanup the url and redact any circle-token in the query string
func cleanURL(req *http.Request) url.URL {
	u := *req.URL
	if u.Scheme == "" {
		u.Scheme = "http"
		if req.TLS != nil {
			u.Scheme = "https"
		}
	}
	return RedactQueryString(u)
}
