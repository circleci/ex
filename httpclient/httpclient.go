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

type Config struct {
	Name                  string
	BaseURL               string
	AuthToken             string
	AcceptType            string
	Timeout               time.Duration
	MaxConnectionsPerHost int
}

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

func New(cfg Config) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxConnsPerHost = cfg.MaxConnectionsPerHost

	c := &Client{
		name:                  cfg.Name,
		baseURL:               cfg.BaseURL,
		backOffMaxInterval:    10 * time.Second,
		backOffMaxElapsedTime: cfg.Timeout,
		authToken:             cfg.AuthToken,
		acceptType:            cfg.AcceptType,
		httpClient: &http.Client{
			Transport: t,
		},
	}

	return c
}

// CloseIdleConnections is never really needed in production with our persistent clients
// but it is handy for testing.
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

// SetAuthHeader will cause the client to use this header instead of standard
// bearer token auth.
func (c *Client) SetAuthHeader(h string) {
	c.authHeader = h
}

type Decoder func(r io.Reader) error

type Request struct {
	Method  string
	Route   string
	Body    interface{}
	Decoder Decoder
	Cookie  *http.Cookie
	Headers map[string]string
	Timeout time.Duration
	Query   url.Values

	url string
}

func NewRequest(method, route string, timeout time.Duration, routeParams ...interface{}) Request {
	return Request{
		Method:  method,
		url:     fmt.Sprintf(route, routeParams...),
		Route:   route,
		Timeout: timeout,
	}
}

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
		defer res.Body.Close()

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
	}
	return nil
}

// NewJSONDecoder returns a decoder func enclosing the resp param
// the func returned takes an io reader which will be passed rto a json decoder to
// decode into the resp.
func NewJSONDecoder(resp interface{}) Decoder {
	return func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(resp); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		return nil
	}
}

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

func IsRequestProblem(err error) bool {
	e := &HTTPError{}
	if errors.As(err, &e) {
		return e.code >= 400 && e.code < 500
	}
	return false
}
