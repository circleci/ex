package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/http/httpproxy"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	o11yconfig "github.com/circleci/ex/config/o11y"
	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpclient/dnscache"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
	"github.com/circleci/ex/o11y/wrappers/o11ynethttp"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/jaeger"
	"github.com/circleci/ex/testing/testcontext"
)

func TestClient_Call_Propagates(t *testing.T) {
	traceIDChan := make(chan string, 1)
	defer close(traceIDChan)

	t.Run("hc-propagation", func(t *testing.T) {
		ctx := testcontext.Background()

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			helpers := o11y.FromContext(ctx).Helpers()
			traceID, _ := helpers.TraceIDs(r.Context())
			traceIDChan <- traceID
			span := o11y.FromContext(r.Context()).GetSpan(r.Context())
			span.AddField("prov_get_span", true)
		})

		server := httptest.NewServer(o11ynethttp.Middleware(o11y.FromContext(ctx), "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:    "hc-test",
			BaseURL: server.URL,
			Timeout: time.Second,
		})

		helpers := o11y.FromContext(ctx).Helpers()

		ctx, span := o11y.StartSpan(ctx, "test client span")
		err := client.Call(ctx, httpclient.NewRequest("POST", "/"))
		assert.Check(t, err)
		span.End()

		httpClientTraceID, _ := helpers.TraceIDs(ctx)

		t.Logf("httpClientTraceID: %q", httpClientTraceID)
		assert.Check(t, cmp.Equal(httpClientTraceID, <-traceIDChan))

		t.Run("no-propagation", func(t *testing.T) {
			err := client.Call(ctx, httpclient.NewRequest("POST", "/", httpclient.Propagation(false)))
			assert.Check(t, err)

			// assert a new traceID was created in the server
			assert.Check(t, httpClientTraceID != <-traceIDChan)
		})
	})

	t.Run("both otel propagation", func(t *testing.T) {
		op, err := otel.New(otel.Config{})
		assert.NilError(t, err)
		ctx := o11y.WithProvider(context.Background(), op)

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			helpers := op.Helpers()
			traceID, _ := helpers.TraceIDs(r.Context())
			traceIDChan <- traceID
			span := o11y.FromContext(r.Context()).GetSpan(r.Context())
			span.AddField("prov_get_span", true)
			b := o11y.GetBaggage(r.Context())
			assert.Check(t, cmp.Equal(b["test_baggage"], "bag value"))
		})

		server := httptest.NewServer(o11ynethttp.Middleware(op, "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:    "otel-test",
			BaseURL: server.URL,
			Timeout: time.Second,
		})

		helpers := o11y.FromContext(ctx).Helpers()

		ctx, span := o11y.StartSpan(ctx, "new client span")
		ctx = o11y.WithBaggage(ctx, o11y.Baggage{
			"test_baggage": "bag value",
		})
		err = client.Call(ctx, httpclient.NewRequest("POST", "/"))
		assert.Check(t, err)
		span.End()

		httpClientTraceID, _ := helpers.TraceIDs(ctx)

		t.Logf("httpClientTraceID: %q", httpClientTraceID)
		assert.Check(t, cmp.Equal(httpClientTraceID, <-traceIDChan))
	})

	t.Run("hc server accepts otel client propagation", func(t *testing.T) {
		srvCtx := testcontext.Background()
		srvProvider := o11y.FromContext(srvCtx)

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			helpers := srvProvider.Helpers()
			traceID, _ := helpers.TraceIDs(r.Context())
			traceIDChan <- traceID
			span := o11y.FromContext(r.Context()).GetSpan(r.Context())
			span.AddField("prov_get_span", true)
		})

		server := httptest.NewServer(o11ynethttp.Middleware(srvProvider, "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:    "otel-test",
			BaseURL: server.URL,
			Timeout: time.Second,
		})

		// Client Side stuff
		op, err := otel.New(otel.Config{})
		assert.NilError(t, err)
		ctx := o11y.WithProvider(context.Background(), op)

		helpers := op.Helpers()

		ctx, span := o11y.StartSpan(ctx, "new client span")
		err = client.Call(ctx, httpclient.NewRequest("POST", "/"))
		assert.Check(t, err)
		span.End()

		httpClientTraceID, _ := helpers.TraceIDs(ctx)

		t.Logf("httpClientTraceID: %q", httpClientTraceID)
		assert.Check(t, cmp.Equal(httpClientTraceID, <-traceIDChan))
	})

	t.Run("hc client propagates to otel server", func(t *testing.T) {
		srvProvider, err := otel.New(otel.Config{})
		assert.NilError(t, err)

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			helpers := srvProvider.Helpers()
			traceID, _ := helpers.TraceIDs(r.Context())
			traceIDChan <- traceID
			span := o11y.FromContext(r.Context()).GetSpan(r.Context())
			span.AddField("prov_get_span", true)
		})

		server := httptest.NewServer(o11ynethttp.Middleware(srvProvider, "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:    "otel-test",
			BaseURL: server.URL,
			Timeout: time.Second,
		})

		// Client Side stuff
		ctx := testcontext.Background()
		op := o11y.FromContext(ctx)
		helpers := op.Helpers()

		ctx, span := o11y.StartSpan(ctx, "new client span")
		err = client.Call(ctx, httpclient.NewRequest("POST", "/"))
		assert.Check(t, err)
		span.End()

		httpClientTraceID, _ := helpers.TraceIDs(ctx)

		t.Logf("httpClientTraceID: %q", httpClientTraceID)
		assert.Check(t, cmp.Equal(httpClientTraceID, <-traceIDChan))
	})

	t.Run("hc client w3c disabled", func(t *testing.T) {
		srvProvider, err := otel.New(otel.Config{})
		assert.NilError(t, err)

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			helpers := srvProvider.Helpers()
			traceID, _ := helpers.TraceIDs(r.Context())
			traceIDChan <- traceID
			span := o11y.FromContext(r.Context()).GetSpan(r.Context())
			span.AddField("prov_get_span", true)
		})

		server := httptest.NewServer(o11ynethttp.Middleware(srvProvider, "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:                       "otel-test",
			BaseURL:                    server.URL,
			Timeout:                    time.Second,
			DisableW3CTracePropagation: true,
		})

		// Client Side stuff
		ctx := testcontext.Background()
		op := o11y.FromContext(ctx)
		helpers := op.Helpers()

		ctx, span := o11y.StartSpan(ctx, "new client span")
		err = client.Call(ctx, httpclient.NewRequest("POST", "/"))
		assert.Check(t, err)
		span.End()

		httpClientTraceID, _ := helpers.TraceIDs(ctx)

		// The server trace id should be different from the client one
		assert.Check(t, httpClientTraceID != <-traceIDChan)
	})

	t.Run("propagate flatten", func(t *testing.T) {
		start := time.Now()
		s := fakestatsd.New(t)
		ctx := testcontext.Background()

		ctx, closeProvider, err := o11yconfig.Otel(ctx, o11yconfig.OtelConfig{
			Dataset:         "propagate-flatten",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main",
			Version:         "dev-test",
			Statsd:          s.Addr(),
			StatsNamespace:  "test-app",
		})

		assert.NilError(t, err)

		okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, span := o11y.StartSpan(r.Context(), "h-span")
			span.AddField("prov_get_span", true)
		})

		server := httptest.NewServer(o11ynethttp.Middleware(o11y.FromContext(ctx), "name", okHandler))
		client := httpclient.New(httpclient.Config{
			Name:    "otel-test",
			BaseURL: server.URL,
			Timeout: time.Second,
		})

		ctx, span := o11y.StartSpan(ctx, "new client span")

		err = client.Call(ctx, httpclient.NewRequest("POST", "/", httpclient.Flatten("tcl")))
		assert.Check(t, err)

		span.End()

		// Should flush to jaeger
		closeProvider(ctx)

		jc := jaeger.New("http://localhost:16686", "app-main")
		traces, err := jc.Traces(ctx, start)
		assert.NilError(t, err)
		assert.Assert(t, cmp.Len(traces, 1))

		spans := traces[0].Spans
		// only one span - no httpcl or http server spans
		assert.Check(t, cmp.Equal(len(spans), 1))

		js := spans[0]
		assert.Check(t, cmp.Equal(js.OperationName, "new client span"))

		expected := map[string]bool{
			"service":                             true,
			"version":                             true,
			"hc_tcl.meta.type":                    true,
			"hc_tcl.http.url":                     true,
			"hc_tcl.http.request_content_length":  true,
			"hc_tcl.http.base_url":                true,
			"hc_tcl.http.target":                  true,
			"hc_tcl.http.retry":                   true,
			"hc_tcl.result":                       true,
			"hc_tcl.flattened":                    true,
			"hc_tcl.http.client_name":             true,
			"hc_tcl.duration_ms":                  true,
			"hc_tcl.http.attempt":                 true,
			"hc_tcl.http.user_agent":              true,
			"hc_tcl.http.response_content_length": true,
			"hc_tcl.http.status_code":             true,
			"hc_tcl.http.route":                   true,
			"hc_tcl.http.scheme":                  true,
			"hc_tcl.http.host":                    true,
			"hc_tcl.http.method":                  true,
			"span.kind":                           true,
			"internal.span.format":                true,

			// N.B. these are added by the jaegertracing/all-in-one:latest image. If you see different results than CI,
			// it's best to do a docker-compose pull to ensure you're using the latest image.
			"otel.scope.name": true,
		}

		assert.Check(t, cmp.Len(js.Tags, len(expected)))
		for _, tag := range js.Tags {
			assert.Check(t, expected[tag.Key], "%q not expected", tag.Key)
		}
	})
}

func TestClient_Call_Decodes(t *testing.T) {
	ctx := testcontext.Background()
	// language=json
	const body = `{"a": "value-a", "b": "value-b"}`

	router := ginrouter.Default(ctx, "httpclient")
	router.POST("/ok", func(c *gin.Context) {
		c.Data(200, "application/json", []byte(body))
	})
	router.POST("/bad", func(c *gin.Context) {
		c.Data(400, "application/json", []byte(body))
	})

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "name",
		BaseURL: server.URL,
		Timeout: time.Second,
	})

	t.Run("Decode JSON", func(t *testing.T) {
		m := make(map[string]string)
		err := client.Call(ctx, httpclient.NewRequest("POST", "/ok",
			httpclient.JSONDecoder(&m),
		))
		assert.Check(t, err)
		assert.Check(t, cmp.DeepEqual(m, map[string]string{
			"a": "value-a",
			"b": "value-b",
		}))
	})

	t.Run("Decode bytes", func(t *testing.T) {
		var bs []byte
		err := client.Call(ctx, httpclient.NewRequest("POST", "/ok",
			httpclient.BytesDecoder(&bs),
		))
		assert.Check(t, err)
		assert.Check(t, cmp.DeepEqual(bs, []byte(body)))
	})

	t.Run("Decode string", func(t *testing.T) {
		var s string
		var respContentType string
		err := client.Call(ctx, httpclient.NewRequest("POST", "/ok",
			httpclient.StringDecoder(&s),
			httpclient.ResponseHeader(func(header http.Header) {
				respContentType = header.Get("Content-Type")
			}),
		))
		assert.Check(t, err)
		assert.Check(t, cmp.Equal(s, body))
		assert.Check(t, cmp.Equal(respContentType, "application/json"))

	})

	t.Run("Decode errors", func(t *testing.T) {
		var s string
		err := client.Call(ctx, httpclient.NewRequest("POST", "/bad",
			httpclient.Decoder(400, httpclient.NewStringDecoder(&s)),
		))
		assert.Check(t, httpclient.HasStatusCode(err, 400))
		assert.Check(t, cmp.Equal(s, body))
	})
}

func TestClient_Call_DialContext(t *testing.T) {
	ctx := testcontext.Background()

	client := httpclient.New(httpclient.Config{
		Name:    "test",
		BaseURL: "https://checkip.amazonaws.com",
		Timeout: time.Second,
		// Wire in the DNS cache
		DialContext: dnscache.DialContext(dnscache.New(dnscache.Config{}), nil),
	})

	s := ""
	err := client.Call(ctx, httpclient.NewRequest("GET", "/",
		httpclient.StringDecoder(&s),
	))
	assert.Check(t, err)
	assert.Check(t, net.ParseIP(strings.TrimSpace(s)) != nil)
}

func TestClient_Call_NoRetry(t *testing.T) {
	ctx := testcontext.Background()

	var counter int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt64(&counter, 1)
		if count == 1 {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "test",
		BaseURL: srv.URL,
		Timeout: time.Second,
		// Wire in the DNS cache
		DialContext: dnscache.DialContext(dnscache.New(dnscache.Config{}), nil),
	})

	err := client.Call(ctx, httpclient.NewRequest("GET", "/",
		httpclient.NoRetry(),
	))
	assert.Check(t, cmp.ErrorContains(err, "500 (Internal Server Error)"))
	assert.Check(t, cmp.Equal(atomic.LoadInt64(&counter), int64(1)))
}

func TestClient_Call_UnixSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	defer cancel()

	router := ginrouter.Default(ctx, "httpclient")
	router.GET("/ok", func(c *gin.Context) {
		c.String(http.StatusOK, "hello unix socket")
	})

	socket := filepath.Join(os.TempDir(), "httpclient-test.sock")

	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server-unix",
		Addr:    socket,
		Handler: router,
		Network: "unix",
	})
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	t.Cleanup(func() {
		assert.Check(t, g.Wait())
	})
	g.Go(func() error {
		return srv.Serve(ctx)
	})

	client := httpclient.New(httpclient.Config{
		Name:      "name",
		BaseURL:   "http://localhost",
		Timeout:   time.Second,
		Transport: httpclient.UnixTransport(socket),
	})

	t.Run("Decode String", func(t *testing.T) {
		s := ""
		err := client.Call(ctx, httpclient.NewRequest("GET", "/ok",
			httpclient.SuccessDecoder(httpclient.NewStringDecoder(&s)),
		))
		assert.Check(t, err)
		assert.Check(t, cmp.Equal("hello unix socket", s))
	})
}

func TestClient_Call_NoContent(t *testing.T) {
	ctx := testcontext.Background()

	okHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}

	server := httptest.NewServer(http.HandlerFunc(okHandler))
	client := httpclient.New(httpclient.Config{
		Name:    "name",
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	type res struct {
		A string `json:"a"`
		B string `json:"b"`
	}
	var m res
	err := client.Call(ctx, httpclient.NewRequest("POST", "/",
		httpclient.JSONDecoder(&m),
	))
	assert.Check(t, errors.Is(err, httpclient.ErrNoContent))
	assert.Check(t, httpclient.IsNoContent(err))
	assert.Check(t, cmp.DeepEqual(m, res{}))
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
			ctx := testcontext.Background()
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			client := httpclient.New(httpclient.Config{
				Name:    tt.name,
				BaseURL: server.URL,
				Timeout: tt.totalTimeout,
			})
			err := client.Call(ctx, httpclient.NewRequest("POST", "/",
				httpclient.Timeout(tt.perRequestTimeout),
			))
			if tt.wantError == nil {
				assert.Check(t, err)
			} else {
				assert.Check(t, errors.Is(err, tt.wantError), err.Error())
			}
		})
	}
}

func TestClient_Call_MaxElapsedTime(t *testing.T) {
	okHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}
	retriedHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}

	tests := []struct {
		name           string
		handler        func(w http.ResponseWriter, r *http.Request)
		maxElapsedTime time.Duration
		wantStatusCode int
	}{
		{
			name:    "good response",
			handler: okHandler,
		},
		{
			name:           "maxElapsedTime error",
			handler:        retriedHandler,
			maxElapsedTime: 200 * time.Millisecond,
			wantStatusCode: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testcontext.Background()
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			client := httpclient.New(httpclient.Config{
				Name:    tt.name,
				BaseURL: server.URL,
				Timeout: 30 * time.Second,
			})

			start := time.Now()
			perRequestTimeout := 50 * time.Millisecond
			err := client.Call(ctx, httpclient.NewRequest("POST", "/",
				httpclient.MaxElapsedTime(tt.maxElapsedTime),
				httpclient.Timeout(perRequestTimeout),
			))

			if tt.wantStatusCode == 0 {
				assert.NilError(t, err)
			} else {
				httpErr := &httpclient.HTTPError{}
				// Checking we have a HTTPError proves we aren't hitting the per req timeout as that results in a
				// context.DeadlineExceeded
				assert.Check(t, errors.As(err, &httpErr))
				assert.Check(t, httpclient.HasStatusCode(err, tt.wantStatusCode))

				// Check the timings are in bounds makes sure we aren't hitting the clients set timeout
				assert.Check(t, time.Since(start) <= tt.maxElapsedTime)
				assert.Check(t, time.Since(start) > perRequestTimeout)
			}
		})
	}
}

func TestClient_Call_Retry500(t *testing.T) {
	ctx := testcontext.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	client := httpclient.New(httpclient.Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	err := client.Call(ctx, httpclient.NewRequest("POST", "/",
		httpclient.Timeout(time.Millisecond),
	))
	// confirm it is still an http error carrying the expected code
	assert.Check(t, httpclient.HasStatusCode(err, http.StatusInternalServerError), err)
	// confirm that it is now not a warning
	assert.Check(t, !o11y.IsWarning(err))
}

func TestClient_Call_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Minute)
		w.WriteHeader(200)
	}))

	client := httpclient.New(httpclient.Config{
		Name:    "context-cancel",
		BaseURL: server.URL,
		Timeout: 10 * time.Second,
	})
	req := httpclient.NewRequest("POST", "/",
		httpclient.Timeout(time.Minute),
	)
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
	ctx := context.Background()
	recorder := httprecorder.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = recorder.Record(r)
		w.WriteHeader(200)
	}))

	client := httpclient.New(httpclient.Config{
		Name:      "context-cancel",
		BaseURL:   server.URL,
		Timeout:   10 * time.Second,
		UserAgent: "Foo",
	})
	err := client.Call(ctx, httpclient.NewRequest("POST", "/",
		httpclient.QueryParam("foo", "bar"),
	))
	assert.Check(t, err)
	assert.Check(t, cmp.DeepEqual(recorder.LastRequest(), &httprecorder.Request{
		Method: "POST",
		URL:    url.URL{Path: "/", RawQuery: "foo=bar"},
		Header: http.Header{
			"Accept-Encoding": {"gzip"},
			"Content-Length":  {"0"},
			"User-Agent":      {"Foo"},
			// since there is no o11y provider on the context there should be no propagation headers
		},
		Body: []uint8{},
	}))
}

func TestClient_Call_SetRawQuery(t *testing.T) {
	ctx := context.Background()
	recorder := httprecorder.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = recorder.Record(r)
		w.WriteHeader(200)
	}))

	client := httpclient.New(httpclient.Config{
		Name:      "context-cancel",
		BaseURL:   server.URL,
		Timeout:   10 * time.Second,
		UserAgent: "Foo",
	})
	err := client.Call(ctx, httpclient.NewRequest("POST", "/",
		httpclient.QueryParam("foo", "banana"),
		httpclient.QueryParam("extra", "value"),
		httpclient.RawQuery("foo=bar"),
	))
	assert.Check(t, err)
	assert.Check(t, cmp.DeepEqual(recorder.LastRequest(), &httprecorder.Request{
		Method: "POST",
		URL:    url.URL{Path: "/", RawQuery: "foo=bar"},
		Header: http.Header{
			"Accept-Encoding": {"gzip"},
			"Content-Length":  {"0"},
			"User-Agent":      {"Foo"},
			// since there is no o11y provider on the context there should be no propagation headers
		},
		Body: []uint8{},
	}))
}

func TestClient_ConnectionPool(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())

	// start our server with a handler that writes a response
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"hello": "world!"} ...`)
		// to help the client have the full number of concurrent requests in flight
		time.Sleep(2 * time.Millisecond)
	})
	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server",
		Addr:    "localhost:0",
		Handler: h,
	})
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	t.Cleanup(func() {
		cancel()
		assert.Check(t, g.Wait())
	})
	g.Go(func() error {
		return srv.Serve(ctx)
	})

	t.Run("keep-alive", func(t *testing.T) {
		// Fire a few requests at the server
		client := httpclient.New(httpclient.Config{
			Name:    "keep-alive",
			BaseURL: "http://" + srv.Addr(),
			Timeout: time.Second,
		})
		req := httpclient.NewRequest("POST", "/")

		for n := 0; n < 50; n++ {
			err := client.Call(context.Background(), req)
			assert.Assert(t, err)
		}

		// all sequential requests should have reused a single connection
		assert.Check(t, cmp.Equal(srv.MetricsProducer().Gauges(ctx)["total_connections"], float64(1)))
	})

	t.Run("connection-reuse", func(t *testing.T) {
		// record the number of connections previous tests have added to the server so far
		startingServerConnections := int(srv.MetricsProducer().Gauges(ctx)["total_connections"])

		maxConnections := 15
		// Fire 100 requests at the server
		client := httpclient.New(httpclient.Config{
			Name:                  "keep-alive",
			BaseURL:               "http://" + srv.Addr(),
			Timeout:               time.Second,
			MaxConnectionsPerHost: maxConnections,
		})
		req := httpclient.NewRequest("POST", "/")

		concurrency := 30
		var wg sync.WaitGroup
		wg.Add(concurrency)
		for c := 0; c < concurrency; c++ {
			go func() {
				for n := 0; n < 10; n++ {
					err := client.Call(testcontext.Background(), req)
					assert.Assert(t, err)
					// This delay increases the effect of not setting MaxIdleConnsPerHost
					// on the client since this increases the chance that each connection may be
					// considered idle and therefore be closed and a new connection created.
					time.Sleep(time.Millisecond)
				}
				wg.Done()
			}()
		}
		wg.Wait()

		// The total new connections made should be at least as much as the max (concurrent) connections
		// since that is lower than the number of concurrent requests. If we were not allowing as many
		// idle connections we would see more total connections made (since they would have been closed
		// and recreated). The server should only see maxConnections made since we allow the same number
		// of idle connections hence we expect to not close and reopen any established connections.
		// (remove the count of connections made in previous tests)
		totalNewConnectionsMade := int(srv.MetricsProducer().Gauges(ctx)["total_connections"]) - startingServerConnections

		// Since the test is non deterministic (depending on the environment it is running in)
		// we may drop some connections, but we should be using most of the pool
		assert.Check(t, totalNewConnectionsMade >= maxConnections-5,
			"made less connections (%d) than expected (%d)", totalNewConnectionsMade, maxConnections)

		// but we would not expect a huge number of dropped connections
		assert.Check(t, totalNewConnectionsMade < maxConnections+5,
			"made mre connections (%d) than expected (%d)", totalNewConnectionsMade, maxConnections+5)
	})
}

func TestClient_RawBody(t *testing.T) {
	ctx := context.Background()
	r := ginrouter.Default(ctx, "raw body")
	r.POST("/", func(c *gin.Context) {
		bs, err := io.ReadAll(c.Request.Body)
		assert.Assert(t, err)
		c.Data(200, "application/octet-stream", bs)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "raw body",
		BaseURL: server.URL,
	})

	bs := []byte("madoka")
	var resp []byte

	req := httpclient.NewRequest("POST", "/", httpclient.RawBody(bs), httpclient.BytesDecoder(&resp))
	err := client.Call(ctx, req)
	assert.Assert(t, err)
	assert.Check(t, cmp.DeepEqual(bs, resp))
}

func TestClient_Proxies(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	t.Cleanup(cancel)

	proxy := startFwdProxy(t)

	// start our server with a handler that writes a response
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"hello": "world!"} ...`)
		// to help the client have the full number of concurrent requests in flight
		time.Sleep(2 * time.Millisecond)
	})
	server := httptest.NewServer(h)

	// default transport proxy is not used for 127.0.0.1 so use a name
	serverURL, _ := url.Parse(server.URL)
	localURL := "http://local.com:" + serverURL.Port()

	t.Run("proxied", func(t *testing.T) {

		t.Setenv("HTTP_PROXY", proxy.URL)
		// can't use the default transport here - since ProxyFromEnvironment
		// detects proxy settings once when first round-tripped
		// similarly we can't use ProxyFromEnvironment here for the same reason

		// similar to the default transport proxy lookup from env
		pf := func(req *http.Request) (*url.URL, error) {
			return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
		}

		client := httpclient.New(httpclient.Config{
			Name:        "proxy",
			BaseURL:     localURL,
			DialContext: localhostDialler(),
			Transport: &http.Transport{
				Proxy: pf,
			},
		})

		req := httpclient.NewRequest("GET", "/path1/path2")
		err := client.Call(ctx, req)
		assert.Assert(t, err)

		// assert that the proxy server was used
		prxURL, _ := url.Parse(proxy.ProxiedURL)
		host, _, _ := net.SplitHostPort(prxURL.Host)
		assert.Check(t, cmp.Equal(host, "local.com"))
		assert.Check(t, cmp.Equal(prxURL.Path, "/path1/path2"))

		t.Run("force_no_proxy", func(t *testing.T) {
			// the client may use the transport with the result of an earlier call to ProxyFromEnvironment
			// so may not honour the env var set in the parent test but we set the prosy nil here explicitly
			client := httpclient.New(httpclient.Config{
				Name:        "no-proxy",
				BaseURL:     localURL,
				DialContext: localhostDialler(),
				TransportModifier: func(t *http.Transport) {
					t.Proxy = nil
				},
			})

			// make a call and confirm that the proxy was not used
			proxy.ProxiedURL = ""
			req := httpclient.NewRequest("GET", "/path3/path4")
			err := client.Call(ctx, req)
			assert.Assert(t, err)

			assert.Check(t, cmp.Equal(proxy.ProxiedURL, ""))
		})
	})
}

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// localhostDialler is a dialer that only dials 127.0.0.1 for any addr
func localhostDialler() dialFunc {
	baseDial := (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, p, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		return baseDial(ctx, network, net.JoinHostPort("127.0.0.1", p))
	}
}

func TestClient_BodyAndRawBody(t *testing.T) {
	ctx := context.Background()
	r := ginrouter.Default(ctx, "body and raw body")
	r.POST("/", func(c *gin.Context) {
		bs, err := io.ReadAll(c.Request.Body)
		assert.Assert(t, err)
		c.Data(200, "application/octet-stream", bs)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "body and raw body",
		BaseURL: server.URL,
	})

	bs := []byte("madoka")
	var resp []byte

	req := httpclient.NewRequest("POST", "/", httpclient.RawBody(bs), httpclient.Body([]byte("test")),
		httpclient.BytesDecoder(&resp))
	err := client.Call(ctx, req)
	assert.Check(t, cmp.Error(err, "cannot have both body and raw body be set"))
}

func TestClient_GETWithBody(t *testing.T) {
	ctx := context.Background()
	r := ginrouter.Default(ctx, "get with body")
	r.POST("/", func(c *gin.Context) {
		bs, err := io.ReadAll(c.Request.Body)
		assert.Assert(t, err)
		c.Data(200, "application/octet-stream", bs)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "get with body",
		BaseURL: server.URL,
	})

	var resp []byte

	req := httpclient.NewRequest("GET", "/", httpclient.Body([]byte("test")),
		httpclient.BytesDecoder(&resp))
	err := client.Call(ctx, req)
	assert.Check(t, cmp.Error(err, "cannot have GET request with body or raw body"))
}

func TestClient_Call_429(t *testing.T) {
	ctx := testcontext.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	tests := []struct {
		name           string
		disableBackoff bool
	}{
		{
			name: "with backoff",
		},
		{
			name:           "without backoff",
			disableBackoff: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := httpclient.New(httpclient.Config{
				BaseURL:            server.URL,
				Timeout:            5 * time.Second,
				NoRateLimitBackoff: tt.disableBackoff,
			})
			err := client.Call(ctx, httpclient.NewRequest("POST", "/",
				httpclient.Timeout(5*time.Second),
			))
			// confirm we get an http 429 error on first call
			assert.Check(t, httpclient.HasStatusCode(err, http.StatusTooManyRequests), err)
			// confirm that it is now not a warning
			assert.Check(t, !o11y.IsWarning(err))

			err = client.Call(ctx, httpclient.NewRequest("POST", "/",
				httpclient.Timeout(5*time.Second),
			))
			if tt.disableBackoff {
				// confirm we still get an http 429 error after receiving the first 429
				assert.Check(t, httpclient.HasStatusCode(err, http.StatusTooManyRequests), err)
			} else {
				// confirm we get a backoff error once the circuit breaker has tripped
				assert.Check(t, errors.Is(err, httpclient.ErrServerBackoff))
			}
			// confirm that it is now not a warning
			assert.Check(t, !o11y.IsWarning(err))
		})
	}
}
