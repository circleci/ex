package httpclient_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/honeycombio/beeline-go/propagation"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/o11ynethttp"
	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/testcontext"
)

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
	client := httpclient.New(httpclient.Config{
		Name:    "name",
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	req := httpclient.NewRequest("POST", "/")

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
		err := client.Call(ctx, httpclient.NewRequest("POST", "/ok",
			httpclient.StringDecoder(&s),
		))
		assert.Check(t, err)
		assert.Check(t, cmp.Equal(s, body))
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
			"Accept-Encoding":                      {"gzip"},
			"Content-Length":                       {"0"},
			"User-Agent":                           {"Foo"},
			propagation.TracePropagationHTTPHeader: {""},
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
			assert.NilError(t, err)
		}

		// all sequential requests should have reused a single connection
		assert.Equal(t, srv.MetricsProducer().Gauges(ctx)["total_connections"], float64(1))
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
					assert.NilError(t, err)
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
		assert.NilError(t, err)
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
	assert.NilError(t, err)
	assert.Check(t, cmp.DeepEqual(bs, resp))
}
