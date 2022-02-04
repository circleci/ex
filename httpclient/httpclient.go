// Package httpclient provides an HTTP client instrumented with the ex/o11y package, it
// includes resiliency behaviour such as configurable timeouts, retries, authentication
// and connection pooling, with support for backing off when a 429 response code is seen.
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
	"strconv"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/honeycombio/beeline-go/propagation"

	"github.com/circleci/ex/o11y"
)

const JSON = "application/json; charset=utf-8"

var (
	ErrNoContent     = o11y.NewWarning("no content")
	ErrServerBackoff = errors.New("server requested explicit backoff")
)

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
	// Timeout is the maximum time any call can take including any retries.
	// Note that a zero Timeout is not defaulted, but means the client will retry indefinitely.
	Timeout time.Duration
	// MaxConnectionsPerHost sets the connection pool size
	MaxConnectionsPerHost int
}

// Client is the o11y instrumented http client.
type Client struct {
	name                  string
	baseURL               string
	httpClient            *http.Client
	backOffMaxElapsedTime time.Duration
	authToken             string
	authHeader            string
	acceptType            string

	mu      sync.RWMutex
	last429 time.Time

	now func() time.Time // purely a test hook
}

// New creates a client configured with the config param
func New(cfg Config) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.MaxConnectionsPerHost == 0 {
		cfg.MaxConnectionsPerHost = 10
	}
	t.MaxConnsPerHost = cfg.MaxConnectionsPerHost
	t.MaxIdleConnsPerHost = cfg.MaxConnectionsPerHost

	return &Client{
		name:                  cfg.Name,
		baseURL:               cfg.BaseURL,
		backOffMaxElapsedTime: cfg.Timeout,
		authHeader:            cfg.AuthHeader,
		authToken:             cfg.AuthToken,
		acceptType:            cfg.AcceptType,
		httpClient: &http.Client{
			Transport: t,
		},
		now: time.Now,
	}
}

// CloseIdleConnections is only used for testing.
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

type Decoder func(r io.Reader) error

// Request is an individual http request that the Client will send
type Request struct {
	Method        string
	Route         string
	Body          interface{} // If set this will be sent as JSON
	Decoder       Decoder     // If set will be used to decode the response body
	Cookie        *http.Cookie
	Headers       map[string]string
	Timeout       time.Duration // The individual per call timeout
	Query         url.Values
	NoPropagation bool

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

	err = c.retryRequest(ctx, spanName, r, newRequestFn)
	// remove the special retry status to resume normal error/warning behaviour
	return doneRetrying(err)
}

// retryRequest will make the request and only call the decoder when a 2XX has been received.
// Any response body in non 2XX cases is discarded.
// nolint: funlen
func (c *Client) retryRequest(ctx context.Context, name string, r Request, newReq func() (*http.Request, error)) error {
	attemptCounter := 0
	attempt := func() (err error) {
		_, span := o11y.StartSpan(ctx, name)
		defer o11y.End(span, &err)
		before := time.Now()

		attemptCounter++

		if c.shouldBackoff() {
			return backoff.Permanent(ErrServerBackoff)
		}

		req, err := newReq()
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
		if !r.NoPropagation {
			req.Header.Add(propagation.TracePropagationHTTPHeader, span.SerializeHeaders())
		}

		span.AddRawField("http.client_name", c.name)
		span.AddRawField("http.route", r.Route)
		span.AddRawField("http.base_url", c.baseURL)
		addReqToSpan(span, req, attemptCounter)

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
			// this is best-efforts so any errors here are not important
			_, _ = io.Copy(ioutil.Discard, res.Body)
			_ = res.Body.Close()
		}()

		m := o11y.FromContext(ctx).MetricsProvider()
		if m != nil {
			_ = m.TimeInMilliseconds("httpclient",
				float64(time.Since(before).Nanoseconds())/1000000.0,
				[]string{
					"http.client_name:" + c.name,
					"http.route:" + r.Route,
					"http.method:" + r.Method,
					"http.status_code:" + strconv.Itoa(res.StatusCode),
					"http.retry:" + strconv.FormatBool(attemptCounter > 1),
				},
				1,
			)
		}
		addRespToSpan(span, res)

		err = extractHTTPError(req, res, attemptCounter, r.Route)
		if err != nil {
			if HasStatusCode(err, http.StatusTooManyRequests) {
				c.setLast429()
			}
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
	bo.InitialInterval = time.Millisecond * 50
	bo.MaxElapsedTime = c.backOffMaxElapsedTime
	return backoff.Retry(attempt, backoff.WithContext(bo, ctx))
}

func addReqToSpan(span o11y.Span, req *http.Request, attempt int) {
	span.AddRawField("meta.type", "http_client")
	span.AddRawField("span.kind", "Client")
	span.AddRawField("http.scheme", req.URL.Scheme)
	span.AddRawField("http.host", req.URL.Host)
	span.AddRawField("http.target", req.URL.Path)
	span.AddRawField("http.method", req.Method)
	span.AddRawField("http.attempt", attempt)
	span.AddRawField("http.retry", attempt > 1)
	span.AddRawField("http.url", req.URL.String())
	span.AddRawField("http.user_agent", req.UserAgent())
	span.AddRawField("http.request_content_length", req.ContentLength)
}

func addRespToSpan(span o11y.Span, res *http.Response) {
	if cl := res.Header.Get("Content-Length"); cl != "" {
		span.AddRawField("http.response_content_length", cl)
	}
	if ct := res.Header.Get("Content-Type"); ct != "" {
		span.AddRawField("http.response_content_type", ct)
	}
	if ce := res.Header.Get("Content-Encoding"); ce != "" {
		span.AddRawField("http.response_content_encoding", ce)
	}
	span.AddRawField("http.status_code", res.StatusCode)
}

func (c *Client) shouldBackoff() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// If not yet 10 seconds since the last 429
	return c.now().Before(c.last429.Add(time.Second * 10))
}

func (c *Client) setLast429() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.last429 = c.now()
}

// NewJSONDecoder returns a decoder func enclosing the resp param
// the func returned takes an io reader which will be passed to a json decoder to
// decode into the resp.
func NewJSONDecoder(resp interface{}) Decoder {
	return func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(resp); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		return nil
	}
}

// NewBytesDecoder decodes the response body into a byte slice
func NewBytesDecoder(resp *[]byte) Decoder {
	return func(r io.Reader) error {
		bs, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		*resp = bs
		return nil
	}
}

// NewStringDecoder decodes the response body into a string
func NewStringDecoder(resp *string) Decoder {
	return func(r io.Reader) error {
		var bs []byte
		err := NewBytesDecoder(&bs)(r)
		if err != nil {
			return err
		}
		*resp = string(bs)
		return nil
	}
}

// HTTPError represents an error in an HTTP call when the response status code is not 2XX
type HTTPError struct {
	method       string
	route        string
	code         int
	attempts     int
	doneRetrying bool
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
		if !e.doneRetrying {
			return true
		}
		// we often expect to see 401, 403 and 404 (let's pretend 402 does not exist for now)
		return e.code > 400 && e.code <= 404
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

func doneRetrying(err error) error {
	e := &HTTPError{}
	if errors.As(err, &e) {
		e.doneRetrying = true
		return e
	}
	return err
}
