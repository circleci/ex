// Package httpclient provides an http client instrumented with the ex/o11y package, and includes
// configurable timeouts, retries, authentication and connection pooling.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/honeycombio/beeline-go/propagation"

	"github.com/circleci/ex/o11y"
)

const JSON = "application/json; charset=utf-8"

var ErrNoContent = o11y.NewWarning("no content")

// Config provides the client configuration
type Config struct {
	// Name is used to identify the client in spans
	Name string
	// BaseURL is the URL and optional path prefix to the server that this is a client of.
	BaseURL string
	// AuthHeader the name of the header that the AuthToken will be set on. If empty then
	// the AuthToken will be used in a bearer token authorization header
	AuthHeader string
	// AuthToken is the token to use for authentication.
	AuthToken string
	// AcceptType if set will be used to set the Accept header.
	AcceptType string
	// Timeout is the maximum time any call can take including any retries
	Timeout time.Duration
	// MaxConnectionsPerHost sets the connection pool size
	MaxConnectionsPerHost int
}

// Client is the o11y instrumented http client
type Client struct {
	name                  string
	baseURL               string
	httpClient            *http.Client
	backOffMaxInterval    time.Duration
	backOffMaxElapsedTime time.Duration
	authToken             string
	authHeader            string
	acceptType            string
}

// New creates a client configured with the config param
func New(cfg Config) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.MaxConnectionsPerHost == 0 {
		cfg.MaxConnectionsPerHost = 10
	}
	t.MaxConnsPerHost = cfg.MaxConnectionsPerHost
	t.MaxIdleConnsPerHost = cfg.MaxConnectionsPerHost

	c := &Client{
		name:                  cfg.Name,
		baseURL:               cfg.BaseURL,
		backOffMaxInterval:    10 * time.Second,
		backOffMaxElapsedTime: cfg.Timeout,
		authHeader:            cfg.AuthHeader,
		authToken:             cfg.AuthToken,
		acceptType:            cfg.AcceptType,
		httpClient: &http.Client{
			Transport: t,
		},
	}

	return c
}

// CloseIdleConnections is only used for testing.
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

type decoder func(r io.Reader) error

// Request is an individual http request that the Client will send
type Request struct {
	Method  string
	Route   string
	Body    interface{} // If set this will be sent as JSON
	Decoder decoder     // If set will be used to decode the response body
	Cookie  *http.Cookie
	Headers map[string]string
	Timeout time.Duration // The individual per call timeout
	Query   url.Values

	url string
}

// NewRequest should be used to create a new request rather than constructing a Request directly.
// This encourages the user to specify a "route" for the tracing, and avoid high cardinality routes
// (when parts of the url may contain many varying values).
// The returned Request can be further altered before being passed to the client.Call.
func NewRequest(method, route string, timeout time.Duration, routeParams ...interface{}) Request {
	return Request{
		Method:  method,
		url:     fmt.Sprintf(route, routeParams...),
		Route:   route,
		Timeout: timeout,
	}
}

// Call makes the request call. It will trace out a top level span and a span for any retry attempts.
// Retries will be attempted on any 5XX responses.
// If the http call completed with a non 2XX status code then an HTTPError will be returned containing
// details of result of the call.
func (c *Client) Call(ctx context.Context, r Request) (err error) {
	spanName := fmt.Sprintf("httpclient: %s %s", c.name, r.Route)
	// most clients should use NewRequest, but if they created a Request directly
	// use the raw Route
	if r.url == "" {
		r.url = r.Route
	}
	u, err := url.Parse(c.baseURL + r.url)
	if err != nil {
		return err
	}
	u.RawQuery = r.Query.Encode() // returns "" if Query is nil

	ctx, span := o11y.StartSpan(ctx, spanName)
	defer o11y.End(span, &err)
	span.AddRawField("span.kind", "httpclient")
	span.AddRawField("api.client", c.name)
	span.AddRawField("api.route", r.Route)
	span.AddRawField("http.base_url", c.baseURL)
	span.AddRawField("http.url", u.String())
	span.AddRawField("http.method", r.Method)

	newRequestFn := func() (*http.Request, error) {
		req, err := http.NewRequest(r.Method, u.String(), nil)
		if err != nil {
			return nil, err
		}
		if c.authToken != "" {
			if c.authHeader != "" {
				req.Header.Set(c.authHeader, c.authToken)
			} else {
				req.Header.Set("Authorization", "Bearer "+c.authToken)
			}
		}

		for k, v := range r.Headers {
			req.Header.Set(k, v)
		}

		if r.Cookie != nil {
			req.AddCookie(r.Cookie)
		}

		if c.acceptType != "" {
			req.Header.Set("Accept", c.acceptType)
		}

		if r.Body != nil {
			req.Header.Set("Content-Type", JSON)
			b := &bytes.Buffer{}
			err = json.NewEncoder(b).Encode(r.Body)
			if err != nil {
				return nil, fmt.Errorf("could not json encode request: %w", err)
			}
			req.Body = ioutil.NopCloser(b)
		}
		return req, nil
	}

	return c.retryRequest(ctx, r, newRequestFn)
}

// retryRequest will make the request and only call the decoder when a 2XX has been received.
// Any response body in non 2XX cases is discarded.
// nolint: funlen
func (c *Client) retryRequest(ctx context.Context, r Request, newRequestFn func() (*http.Request, error)) error {
	finalStatus := 0
	defer func() {
		// Add the status code from after all retries to the parent span
		o11y.FromContext(ctx).GetSpan(ctx).AddRawField("http.status_code", finalStatus)
	}()

	attemptCounter := 0
	attempt := func() (err error) {
		attemptCounter++
		_, span := o11y.StartSpan(ctx, "httpclient: call")
		defer o11y.End(span, &err)

		req, err := newRequestFn()
		if err != nil {
			return backoff.Permanent(err)
		}

		// Add the per single http request timeout.
		// This client is essentially for service to service calls, anyone is going to expect
		// it to have a sane default timeout, hence 5 seconds if one is not specified.
		requestTimeout := r.Timeout
		if requestTimeout == 0 {
			requestTimeout = time.Second * 5
		}
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		req = req.WithContext(ctx)
		req.Header.Add(propagation.TracePropagationHTTPHeader, span.SerializeHeaders())

		span.RecordMetric(o11y.Timing("httpclient",
			"api.client", "api.route", "http.method", "http.status_code", "http.retry"))
		span.AddField("meta.type", "http_client")
		span.AddRawField("api.client", c.name)
		span.AddRawField("api.route", r.Route)
		span.AddRawField("http.scheme", req.URL.Scheme)
		span.AddRawField("http.host", req.URL.Host)
		span.AddRawField("http.target", req.URL.Path)
		span.AddRawField("http.method", req.Method)
		span.AddRawField("http.attempt", attemptCounter)
		span.AddRawField("http.retry", attemptCounter > 1)

		res, err := c.httpClient.Do(req)
		if err != nil {
			// url errors repeat the method and url which clutters metrics and logging
			e := &url.Error{}
			if errors.As(err, &e) {
				err = e.Err
			}
			return fmt.Errorf("call: %s %s failed with: %w after %d attempt(s)",
				req.Method, r.Route, err, attemptCounter)
		}
		defer func() {
			// drain anything left in the body and close it, to ensure we can take advantage of keep alive
			// this is best efforts so any errors here are not important
			_, _ = io.Copy(ioutil.Discard, res.Body)
			_ = res.Body.Close()
		}()

		finalStatus = res.StatusCode
		if cl := res.Header.Get("Content-Length"); cl != "" {
			span.AddField("response.content_length", cl)
		}
		if ct := res.Header.Get("Content-Type"); ct != "" {
			span.AddField("response.content_type", ct)
		}
		if ce := res.Header.Get("Content-Encoding"); ce != "" {
			span.AddField("response.content_encoding", ce)
		}
		span.AddField("response.status_code", res.StatusCode)

		err = extractHTTPError(req, res, attemptCounter, r.Route)
		if err != nil {
			return err
		}
		if r.Decoder == nil {
			return nil
		}
		err = r.Decoder(res.Body)
		if err != nil {
			// do not retry decoding errors
			return backoff.Permanent(fmt.Errorf("call: %s %s decoding failed with: %w after %d attempt(s)",
				req.Method, r.Route, err, attemptCounter))
		}
		return nil
	}

	bo := backoff.NewExponentialBackOff()
	bo.MaxInterval = c.backOffMaxInterval
	bo.MaxElapsedTime = c.backOffMaxElapsedTime
	return backoff.Retry(attempt, backoff.WithContext(bo, ctx))
}

// NewJSONDecoder returns a decoder func enclosing the resp param
// the func returned takes an io reader which will be passed to a json decoder to
// decode into the resp.
func NewJSONDecoder(resp interface{}) decoder {
	return func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(resp); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		return nil
	}
}

// HTTPError represents an error in an HTTP call when the response status code is not 2XX
type HTTPError struct {
	method   string
	route    string
	code     int
	attempts int
}

var _ error = (*HTTPError)(nil)

func (e *HTTPError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("the response from %s %s was %d (%s) (%d attempts)",
		e.method, e.route, e.code, http.StatusText(e.code), e.attempts)
}

// Code returns the status code recorded in this error.
func (e *HTTPError) Code() int {
	return e.code
}

// Is checks that this error is being checked for the special o11y error that is not
// added to the trace as an error. If the error is due to relatively expected failure response codes
// return true so it does not appear in the traces as an error.
func (e *HTTPError) Is(target error) bool {
	if o11y.IsWarningNoUnwrap(target) {
		// we often expect to see 401, 403 and 404 (let's pretend 402 does not exist for now)
		return e.code > 400 && e.code <= 404
	}
	return false
}

// HasErrorCode is DEPRECATED - use HasStatusCode instead.
func HasErrorCode(err error, codes ...int) bool {
	e := &HTTPError{}
	if errors.As(err, &e) {
		for _, code := range codes {
			if e.code == code {
				return true
			}
		}
	}
	return false
}

// HasStatusCode tests err for HTTPError and returns true if any of the codes
// match the stored code.
func HasStatusCode(err error, codes ...int) bool {
	e := &HTTPError{}
	if errors.As(err, &e) {
		for _, code := range codes {
			if e.code == code {
				return true
			}
		}
	}
	return false
}

// IsRequestProblem checks the err for HTTPError and returns true if the stored status code
// is in the 4xx range
func IsRequestProblem(err error) bool {
	e := &HTTPError{}
	if errors.As(err, &e) {
		return e.code >= 400 && e.code < 500
	}
	return false
}

func IsNoContent(err error) bool {
	return errors.Is(err, ErrNoContent)
}

// extractHTTPError returns an HTTPError if the response status code is >=300, otherwise it
// returns nil.
func extractHTTPError(req *http.Request, res *http.Response, attempts int, route string) error {
	httpErr := &HTTPError{
		method:   req.Method,
		route:    route,
		code:     res.StatusCode,
		attempts: attempts,
	}
	switch {
	case res.StatusCode >= 500:
		// 500 could be temporary server problems, so should retry.
		return httpErr
	case res.StatusCode >= 300:
		// All other none 2XX codes are something we did wrong so exit the retry.
		return backoff.Permanent(httpErr)
	case res.StatusCode == http.StatusNoContent:
		return backoff.Permanent(ErrNoContent)
	}
	return nil
}
