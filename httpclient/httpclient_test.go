package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/honeycombio/beeline-go/propagation"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/o11ynethttp"
	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/testcontext"
)

func TestNewRequest_Formats(t *testing.T) {
	req := NewRequest("POST", "/%s.txt", time.Second, "the-path")
	assert.Check(t, cmp.Equal(req.url, "/the-path.txt"))
	assert.Check(t, cmp.Equal(req.Route, "/%s.txt"))
	assert.Check(t, cmp.Equal(req.Method, "POST"))
	assert.Check(t, cmp.Equal(req.Timeout, time.Second))
}

func TestClient_Call_Propagates(t *testing.T) {
	ctx := testcontext.Background()
	re := regexp.MustCompile(`trace_id=([A-z0-9]+)`)

	traceIDChan := make(chan string, 1)
	defer close(traceIDChan)

	rec := httprecorder.New()
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, span := o11y.StartSpan(ctx, "test server span")
		traceIDChan <- re.FindStringSubmatch(span.SerializeHeaders())[1]
		_ = rec.Record(r)
	})

	server := httptest.NewServer(o11ynethttp.Middleware(o11y.FromContext(ctx), "name", okHandler))
	client := New(Config{
		Name:    "name",
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	req := NewRequest("POST", "/", time.Second)

	ctx, span := o11y.StartSpan(ctx, "test client span")
	err := client.Call(ctx, req)
	assert.Check(t, err)
	span.End()

	h := rec.LastRequest().Header
	assert.Check(t, cmp.Contains(h.Get(propagation.TracePropagationHTTPHeader), "trace_id="))

	httpClientSpanID := re.FindStringSubmatch(span.SerializeHeaders())[1]
	t.Logf("httpClientSpanID: %q", httpClientSpanID)
	httpServerSpanID := re.FindStringSubmatch(h.Get(propagation.TracePropagationHTTPHeader))[1]
	assert.Check(t, cmp.Equal(httpClientSpanID, httpServerSpanID))
	assert.Check(t, cmp.Equal(httpClientSpanID, <-traceIDChan))
}

func TestClient_Call_Decodes(t *testing.T) {
	ctx := testcontext.Background()

	okHandler := func(w http.ResponseWriter, r *http.Request) {
		// language=json
		_, _ = io.WriteString(w, `{"a": "value-a", "b": "value-b"}`)
	}

	server := httptest.NewServer(http.HandlerFunc(okHandler))
	client := New(Config{
		Name:    "name",
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	req := NewRequest("POST", "/", time.Second)

	m := make(map[string]string)
	req.Decoder = NewJSONDecoder(&m)

	err := client.Call(ctx, req)
	assert.Check(t, err)
	assert.Check(t, cmp.DeepEqual(m, map[string]string{
		"a": "value-a",
		"b": "value-b",
	}))
}

func TestClient_Call_Timeouts(t *testing.T) {
	okHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}
	longHandler := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Minute)
		w.WriteHeader(200)
	}

	tests := []struct {
		name              string
		handler           func(w http.ResponseWriter, r *http.Request)
		totalTimeout      time.Duration
		perRequestTimeout time.Duration
		wantError         error
	}{
		{
			name:      "good response",
			handler:   okHandler,
			wantError: nil,
		},
		{
			name:              "timeout with retries",
			handler:           longHandler,
			totalTimeout:      time.Second,
			perRequestTimeout: time.Millisecond,
			wantError:         context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			client := New(Config{
				Name:    tt.name,
				BaseURL: server.URL,
				Timeout: tt.totalTimeout,
			})
			req := NewRequest("POST", "/", tt.perRequestTimeout)
			ctx := testcontext.Background()
			err := client.Call(ctx, req)
			if tt.wantError == nil {
				assert.Check(t, err)
			} else {
				assert.Check(t, errors.Is(err, tt.wantError), err.Error())
			}
		})
	}
}

func TestClient_Call_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Minute)
		w.WriteHeader(200)
	}))

	client := New(Config{
		Name:    "context-cancel",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})
	req := NewRequest("POST", "/", time.Minute)
	ctx, cancel := context.WithCancel(testcontext.Background())
	defer cancel()

	callErr := make(chan error)
	go func() {
		callErr <- client.Call(ctx, req)
	}()

	time.Sleep(time.Millisecond * 10)
	cancel()

	select {
	case <-time.After(time.Second * 5):
		t.Error("context cancellation did not stop the client")
	case err := <-callErr:
		assert.Check(t, errors.Is(err, context.Canceled))
	}
}

func TestClient_Call_SetQuery(t *testing.T) {
	recorder := httprecorder.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = recorder.Record(r)
		w.WriteHeader(200)
	}))

	client := New(Config{
		Name:    "context-cancel",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})
	req := NewRequest("POST", "/", time.Second)
	req.Query = url.Values{}
	req.Query.Set("foo", "bar")

	err := client.Call(context.Background(), req)
	assert.Check(t, err)
	assert.Check(t, cmp.DeepEqual(recorder.LastRequest(), &httprecorder.Request{
		Method: "POST",
		URL:    url.URL{Path: "/", RawQuery: "foo=bar"},
		Header: http.Header{
			"Accept-Encoding":                      {"gzip"},
			"Content-Length":                       {"0"},
			"User-Agent":                           {"Go-http-client/1.1"},
			propagation.TracePropagationHTTPHeader: {""},
		},
		Body: []uint8{},
	}))
}

func TestHTTPError_Is(t *testing.T) {
	tests := []struct {
		code int
		is   bool
	}{
		{code: 100, is: false},
		{code: 101, is: false},
		{code: 400, is: false},
		{code: 401, is: true},
		{code: 403, is: true},
		{code: 404, is: true},
		{code: 405, is: false},
		{code: 500, is: false},
		{code: 503, is: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("code-:%d", tt.code), func(t *testing.T) {
			err := &HTTPError{code: tt.code}
			assert.Check(t, cmp.Equal(o11y.IsWarning(err), tt.is))

			// confirm wrapped it is still checked as a do not trace
			wErr := fmt.Errorf("foo :%w", err)
			assert.Check(t, cmp.Equal(o11y.IsWarning(err), tt.is))

			// and check the wrapped err it still is an HTTPError and that we can get the code back
			ne := &HTTPError{}
			assert.Check(t, errors.As(wErr, &ne))
			assert.Check(t, cmp.Equal(ne.code, tt.code))
			// ne should be equivalent to wErr now
			assert.Check(t, !errors.Is(err, wErr))

			// check that no two instances are Is-quivalent
			err2 := &HTTPError{}
			// and confirm they are not equivalent
			assert.Check(t, !errors.Is(err, err2))
		})
	}
}

func TestHasErrorCode(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		codes []int
		want  bool
	}{
		{
			name: "With matching code",
			err: &HTTPError{
				code: 400,
			},
			codes: []int{400, 500},
			want:  true,
		},
		{
			name: "With different code",
			err: &HTTPError{
				code: 200,
			},
			codes: []int{400, 500},
			want:  false,
		},
		{
			name:  "Empty error",
			err:   &HTTPError{},
			codes: []int{400},
			want:  false,
		},
		{
			name:  "Nil error",
			err:   nil,
			codes: []int{400},
			want:  false,
		},
		{
			name:  "Other kind of error",
			err:   errors.New("some other error"),
			codes: []int{400},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(HasErrorCode(tt.err, tt.codes...), tt.want))
		})
	}
}

func TestIsRequestProblem(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "With problem code",
			err: &HTTPError{
				code: 400,
			},
			want: true,
		},
		{
			name: "With non-request error code",
			err: &HTTPError{
				code: 500,
			},
			want: false,
		},
		{
			name: "With good code",
			err: &HTTPError{
				code: 200,
			},
			want: false,
		},
		{
			name: "Empty error",
			err:  &HTTPError{},
			want: false,
		},
		{
			name: "Nil error",
			err:  nil,
			want: false,
		},
		{
			name: "Other kind of error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Check(t, cmp.Equal(IsRequestProblem(tt.err), tt.want))
		})
	}
}
