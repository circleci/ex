// Package httpclient provides an HTTP client instrumented with the ex/o11y package, it
// includes resiliency behaviour such as configurable timeouts, retries, authentication
// and connection pooling, with support for backing off when a 429 response code is seen.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/circleci/ex/o11y"
)

const JSON = "application/json; charset=utf-8"

var (
	ErrNoContent     = o11y.NewWarning("no content")
	ErrServerBackoff = errors.New("server requested explicit backoff")
)

type tracer interface {
	Wrap(name string, r http.RoundTripper) http.RoundTripper
	WithTracer(ctx context.Context, route string) context.Context
}

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
	// UserAgent that will be used for every request
	UserAgent string
	// Transport allows overriding the default HTTP transport the client will use.
	Transport http.RoundTripper
	// TransportModifier can modify the transport after the client has applied other config settings
	TransportModifier func(Transport *http.Transport)
	// Tracer allows http stats tracing to be enabled.
	Tracer tracer
	// DialContext allows a dial context to be injected into the HTTP transport.
	DialContext func(ctx context.Context, network string, addr string) (net.Conn, error)
	// DisableHTTP2 is used to force a client to use HTTP/1.1 regardless of the server-side support
	DisableHTTP2 bool
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
	additionalHeaders     map[string]string
	tracer                tracer

	mu      sync.RWMutex
	last429 time.Time

	now func() time.Time // purely a test hook
}

// New creates a client configured with the config param
func New(cfg Config) *Client {
	if cfg.Transport == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.MaxConnectionsPerHost == 0 {
			cfg.MaxConnectionsPerHost = 10
		}
		transport.MaxConnsPerHost = cfg.MaxConnectionsPerHost
		transport.MaxIdleConnsPerHost = cfg.MaxConnectionsPerHost
		transport.DialContext = cfg.DialContext
		cfg.Transport = transport
	}
	if cfg.TransportModifier != nil {
		if transport, ok := cfg.Transport.(*http.Transport); ok {
			cfg.TransportModifier(transport)
		}
	}

	additionalHeaders := make(map[string]string)
	if cfg.UserAgent != "" {
		additionalHeaders["User-Agent"] = cfg.UserAgent
	}

	var roundTripper = cfg.Transport
	if cfg.Tracer != nil {
		roundTripper = cfg.Tracer.Wrap(cfg.Name, roundTripper)
	}

	if cfg.DisableHTTP2 {
		if rt, ok := roundTripper.(*http.Transport); ok {
			// disable HTTP/2
			rt.TLSNextProto = map[string]func(authority string, c *tls.Conn) http.RoundTripper{}
		}
	}

	return &Client{
		name:                  cfg.Name,
		baseURL:               cfg.BaseURL,
		backOffMaxElapsedTime: cfg.Timeout,
		authHeader:            cfg.AuthHeader,
		additionalHeaders:     additionalHeaders,
		authToken:             cfg.AuthToken,
		acceptType:            cfg.AcceptType,
		httpClient: &http.Client{
			Transport: roundTripper,
		},
		tracer: cfg.Tracer,
		now:    time.Now,
	}
}

func UnixTransport(socket string) *http.Transport {
	return &http.Transport{
		DialContext: func(_ctx context.Context, _network, _addr string) (net.Conn, error) {
			return net.Dial("unix", socket)
		},
	}
}

// CloseIdleConnections is only used for testing.
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

type decoder func(r io.Reader) error

const successDecodeStatus = -1

// Request is an individual http Request that the Client will send
// NewRequest should be used to create a new Request rather than constructing a Request directly.
type Request struct {
	method  string
	route   string
	body    interface{} // If set this will be sent as JSON
	rawBody []byte      // If set this will be sent as is

	decoders map[int]decoder          // If set will be used to decode the response body by http status code
	headerFn func(header http.Header) // If set will be called with the response header
	cookie   *http.Cookie
	headers  map[string]string
	timeout  time.Duration // The individual per call timeout
	retry    bool
	query    url.Values

	// We want to prevent HTTP GETs with body or rawBody due to incompatibilities with CloudFront WAF which API Infra
	// are introducing. In order to facilitate a migration for runner we need to be able to override this. This can be
	// removed once RT-724 is completed.
	allowGETWithBody bool

	propagation bool

	url string
}

// NewRequest should be used to create a new Request rather than constructing a Request directly.
// This encourages the user to specify a "route" for the tracing, and avoid high cardinality routes
// (when parts of the url may contain many varying values).
//
// Example:
//
//	req := httpclient.NewRequest("POST", "/api/person/%s",
//	  httpclient.RouteParams("person-id"),
//	  httpclient.Timeout(time.Second),
//	)
func NewRequest(method, route string, opts ...func(*Request)) Request {
	r := Request{
		method:      method,
		url:         route,
		route:       route,
		decoders:    map[int]decoder{},
		headers:     map[string]string{},
		query:       url.Values{},
		propagation: true,
		retry:       true,
	}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

func RouteParams(routeParams ...interface{}) func(*Request) {
	return func(r *Request) {
		r.url = fmt.Sprintf(r.route, routeParams...)
	}
}

// AllowGETWithBody will allow the client to send a GET request with a body, which we error on by default. We should
// remove this once RT-724 is completed.
func AllowGETWithBody() func(*Request) {
	return func(r *Request) {
		r.allowGETWithBody = true
	}
}

// Decoder adds a response body decoder to some http status code
// Note this will modify the original Request.
//
// Example:
//
//	err := client.Call(ctx, httpclient.NewRequest("POST", "/bad",
//	  httpclient.Decoder(http.StatusBadRequest, httpclient.NewStringDecoder(&s)),
//	))
func Decoder(status int, decoder decoder) func(*Request) {
	return func(r *Request) {
		r.decoders[status] = decoder
	}
}

// SuccessDecoder sets the decoder for all 2xx statuses
func SuccessDecoder(decoder decoder) func(*Request) {
	return Decoder(successDecodeStatus, decoder)
}

// JSONDecoder is a shorthand to decode the success body as JSON
func JSONDecoder(resp interface{}) func(*Request) {
	return SuccessDecoder(NewJSONDecoder(resp))
}

// StringDecoder is a shorthand to decode the success body as a string
func StringDecoder(resp *string) func(*Request) {
	return SuccessDecoder(NewStringDecoder(resp))
}

// BytesDecoder is a shorthand to decode the success body as raw bytes
func BytesDecoder(resp *[]byte) func(*Request) {
	return SuccessDecoder(NewBytesDecoder(resp))
}

func ResponseHeader(f func(http.Header)) func(*Request) {
	return func(r *Request) {
		r.headerFn = f
	}
}

// Body sets the request body that will be sent as JSON
func Body(body interface{}) func(*Request) {
	return func(r *Request) {
		r.body = body
	}
}

// RawBody sets the request body that will be sent as is
func RawBody(body []byte) func(*Request) {
	return func(r *Request) {
		r.rawBody = body
	}
}

// Cookie sets the cookie for the request
func Cookie(cookie *http.Cookie) func(*Request) {
	return func(r *Request) {
		r.cookie = cookie
	}
}

// Header sets one header value for the request
func Header(key, val string) func(*Request) {
	return func(r *Request) {
		r.headers[key] = val
	}
}

// Headers sets multiple headers in one go for the request
func Headers(headers map[string]string) func(*Request) {
	return func(r *Request) {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
}

// QueryParam sets one query param for the request
func QueryParam(key, value string) func(*Request) {
	return func(r *Request) {
		r.query.Set(key, value)
	}
}

// QueryParams sets multiple query params for the request
func QueryParams(params map[string]string) func(*Request) {
	return func(r *Request) {
		for k, v := range params {
			r.query.Set(k, v)
		}
	}
}

// Timeout sets the individual request timeout,
// and does not take into account of retries.
// This is different from setting the timeout field on the http client,
// which is the total timeout across all retries.
func Timeout(timeout time.Duration) func(*Request) {
	return func(r *Request) {
		r.timeout = timeout
	}
}

// NoRetry prevents any retries from being made for this request.
func NoRetry() func(*Request) {
	return func(r *Request) {
		r.retry = false
	}
}

// Propagation sets the tracing propagation header on the request if set to true,
// the header is not set if set to false
func Propagation(propagation bool) func(*Request) {
	return func(r *Request) {
		r.propagation = propagation
	}
}

// Call makes the Request call. It will trace out a top level span and a span for any retry attempts.
// Retries will be attempted on any 5XX responses.
// If the http call completed with a non 2XX status code then an HTTPError will be returned containing
// details of result of the call.
//
// Example:
//
//	err := client.Call(ctx, httpclient.NewRequest("POST", "/api/fruit/%s",
//	  httpclient.RouteParams("apple"),
//	  httpclient.Timeout(time.Second),
//	))
//	if err != nil {
//	  panic(err)
//	}
//
// nolint:funlen
func (c *Client) Call(ctx context.Context, r Request) (err error) {
	if err := r.validate(); err != nil {
		return err
	}
	spanName := fmt.Sprintf("httpclient: %s %s", c.name, r.route)
	// most clients should use NewRequest, but if they created a Request directly
	// use the raw route
	if r.url == "" {
		r.url = r.route
	}
	u, err := url.Parse(c.baseURL + r.url)
	if err != nil {
		return err
	}
	u.RawQuery = r.query.Encode() // returns "" if query is nil

	newRequestFn := func() (*http.Request, error) {
		req, err := http.NewRequest(r.method, u.String(), nil)
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

		for k, v := range c.additionalHeaders {
			req.Header.Set(k, v)
		}
		for k, v := range r.headers {
			req.Header.Set(k, v)
		}

		if r.cookie != nil {
			req.AddCookie(r.cookie)
		}

		if c.acceptType != "" {
			req.Header.Set("Accept", c.acceptType)
		}

		if r.body != nil {
			req.Header.Set("Content-Type", JSON)
			b := &bytes.Buffer{}
			err = json.NewEncoder(b).Encode(r.body)
			if err != nil {
				return nil, fmt.Errorf("could not json encode Request: %w", err)
			}
			req.Body = io.NopCloser(b)
		}

		if r.rawBody != nil {
			b := bytes.NewReader(r.rawBody)
			req.Body = io.NopCloser(b)
		}

		return req, nil
	}

	err = c.retryRequest(ctx, spanName, r, newRequestFn)
	// remove the special retry status to resume normal error/warning behaviour
	return doneRetrying(err)
}

// retryRequest will make the Request and only call the decoder when a 2XX has been received.
// Any response body in non 2XX cases is discarded.
// nolint: funlen, gocyclo
func (c *Client) retryRequest(ctx context.Context, name string, r Request, newReq func() (*http.Request, error)) error {
	attemptCounter := 0
	// requestClose is used in an attempt to patch over some strange behaviour where the connection used in
	// a request that timed out is kept active in the pool and reused, timing out again.
	requestClose := false
	attempt := func() (err error) {
		ctx, span := o11y.StartSpan(ctx, name)
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
		requestTimeout := r.timeout
		if requestTimeout == 0 {
			requestTimeout = time.Second * 5
		}
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		if c.tracer != nil {
			ctx = c.tracer.WithTracer(ctx, r.route)
		}

		req = req.WithContext(ctx)
		if r.propagation {
			c.addPropagationHeader(ctx, req)
		}
		if requestClose {
			// In the case where we see a timed out connection being reused it if usually the case that
			// the same connection is used in this retry loop, so this will hopefully cause it to close.
			// (If we see this not having an impact - we may still need to force all the pool to reconnect.)
			// TODO - this may only cause the server to drop the connection (and the client only respond to the
			// server side close. In the case of the reused bad connection it is possible the server is not even
			// getting the request and the close header. It remains to be seen how the client behaves in this case)
			// In the case where the client is correctly closing the connection on a timeout, this may cause an
			// extra connection close, which should be safe enough.
			req.Header.Set("Connection", "close")
		}

		span.AddRawField("http.client_name", c.name)
		span.AddRawField("http.route", r.route)
		span.AddRawField("http.base_url", c.baseURL)
		addReqToSpan(span, req, attemptCounter)

		res, err := c.httpClient.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				requestClose = true
			}
			// url errors repeat the method and url which clutters metrics and logging
			e := &url.Error{}
			if errors.As(err, &e) {
				err = e.Err
			}
			return fmt.Errorf("call: %s %s failed with: %w after %d attempt(s)",
				req.Method, r.route, err, attemptCounter)
		}

		defer func() {
			// drain anything left in the body and close it, to ensure we can take advantage of keep alive
			// this is best-efforts so any errors here are not important
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}()

		m := o11y.FromContext(ctx).MetricsProvider()
		if m != nil {
			_ = m.TimeInMilliseconds("httpclient",
				float64(time.Since(before).Nanoseconds())/1000000.0,
				[]string{
					"http.client_name:" + c.name,
					"http.route:" + r.route,
					"http.method:" + r.method,
					"http.status_code:" + strconv.Itoa(res.StatusCode),
					"http.retry:" + strconv.FormatBool(attemptCounter > 1),
				},
				1,
			)
		}
		addRespToSpan(span, res)

		err = extractHTTPError(req, res, attemptCounter, r.route)
		if err != nil {
			if HasStatusCode(err, http.StatusTooManyRequests) {
				c.setLast429()
			}

			errDecode := r.decodeBody(res, false, attemptCounter)
			if errDecode != nil {
				return errDecode
			}

			return err
		}

		if r.headerFn != nil {
			r.headerFn(res.Header)
		}

		err = r.decodeBody(res, true, attemptCounter)
		if err != nil {
			return err
		}

		return nil
	}

	if !r.retry {
		return attempt()
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = time.Millisecond * 50
	bo.MaxElapsedTime = c.backOffMaxElapsedTime
	return backoff.Retry(attempt, backoff.WithContext(bo, ctx))
}

func (c *Client) addPropagationHeader(ctx context.Context, req *http.Request) {
	p := o11y.FromContext(ctx)
	if p == nil {
		return
	}
	propagation := p.Helpers().ExtractPropagation(ctx)
	for k, v := range propagation.Headers {
		req.Header.Set(k, v)
	}
}

func (r Request) decodeBody(resp *http.Response, success bool, attemptCounter int) error {
	var decoder decoder
	code := resp.StatusCode

	if success {
		if d, ok := r.decoders[successDecodeStatus]; ok {
			decoder = d
		}
	}
	if d, ok := r.decoders[code]; ok {
		decoder = d
	}

	if decoder != nil {
		err := decoder(resp.Body)
		if err != nil {
			// do not retry decoding errors
			return backoff.Permanent(fmt.Errorf("call: %s %s decoding failed with: %w after %d attempt(s)",
				r.method, r.route, err, attemptCounter))
		}
	}

	return nil
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
func NewJSONDecoder(resp interface{}) decoder {
	return func(r io.Reader) error {
		if err := json.NewDecoder(r).Decode(resp); err != nil {
			return fmt.Errorf("failed to unmarshal: %w", err)
		}
		return nil
	}
}

// NewBytesDecoder decodes the response body into a byte slice
func NewBytesDecoder(resp *[]byte) decoder {
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
func NewStringDecoder(resp *string) decoder {
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

func (r Request) validate() error {
	// We do not allow GET requests with Body as they are not supported by CloudFront WAF, which requests are routed
	// through. - https://circleci.slack.com/archives/C03M4P0Q4GH/p1659566842825159
	// This can be overridden with httpclient.AllowGETWithBody() if required for legacy or third-party compatibility
	if !r.validateGetWithBody() {
		return errors.New("cannot have GET request with body or raw body")
	}

	if !r.validateOnlyRawBodyOrBody() {
		return errors.New("cannot have both body and raw body be set")
	}

	return nil
}

func (r Request) validateOnlyRawBodyOrBody() bool {
	if r.rawBody != nil && r.body != nil {
		return false
	}

	return true
}

func (r Request) validateGetWithBody() bool {
	if !r.allowGETWithBody && (r.method == "GET" && r.hasBody()) {
		return false
	}

	return true
}

func (r Request) hasBody() bool {
	return r.body != nil || r.rawBody != nil
}
