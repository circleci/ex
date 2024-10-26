package o11ygin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	o11yconfig "github.com/circleci/ex/config/o11y"
	hc "github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/internal/syncbuffer"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/testing/fakemetrics"
	"github.com/circleci/ex/testing/jaeger"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMiddleware(t *testing.T) {
	m := &fakemetrics.Provider{}

	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: m,
	}))
	provider := o11y.FromContext(ctx)
	t.Cleanup(func() {
		provider.Close(ctx)
		assert.Check(t, cmp.DeepEqual(
			[]fakemetrics.MetricCall{
				{
					Metric: "timer",
					Name:   "handler",
					Tags: []string{
						"http.server_name:test-server",
						"http.method:POST",
						"http.route:/api/:id",
						"http.status_code:200",
					},
					Rate: 1,
				},
				{
					Metric: "timer",
					Name:   "httpclient",
					Tags: []string{
						"http.client_name:test-client",
						"http.route:/api/%s",
						"http.method:POST",
						"http.status_code:200",
						"http.retry:false",
					},
					Rate: 1,
				},
				{
					Metric: "timer",
					Name:   "handler",
					Tags: []string{
						"http.server_name:test-server",
						"http.method:POST",
						"http.route:/api/:id",
						"http.status_code:404",
					},
					Rate: 1,
				},
				{
					Metric: "timer",
					Name:   "httpclient",
					Tags: []string{
						"http.client_name:test-client",
						"http.route:/api/%s",
						"http.method:POST",
						"http.status_code:404",
						"http.retry:false",
					},
					Rate: 1,
				},
				{
					Metric: "timer",
					Name:   "handler",
					Tags: []string{
						"http.server_name:test-server",
						"http.method:POST",
						"http.route:/api/:id",
						"http.status_code:500",
					},
					Rate: 1,
				},
				{
					Metric: "count",
					Name:   "panics",
					Tags: []string{
						"name:http-server test-server: POST /api/:id",
					},
					Rate: 1,
				},
				{
					Metric: "timer",
					Name:   "handler",
					Tags: []string{
						"http.server_name:test-server",
						"http.method:POST",
						"http.route:/api/:id",
						"http.status_code:500",
					},
					Rate: 1,
				},
				{
					Metric:   "count",
					Name:     "error",
					ValueInt: 1,
					Tags:     []string{"type:o11y"},
					Rate:     1,
				},

				{
					Metric:   "count",
					Name:     "warning",
					ValueInt: 1,
					Tags:     []string{"type:o11y"},
					Rate:     1,
				},
			},
			m.Calls(), fakemetrics.CMPMetrics, cmpopts.IgnoreFields(fakemetrics.MetricCall{}, "Value", "ValueInt")),
		)
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := gin.New()
	r.Use(
		Middleware(provider, "test-server", nil),
		Recovery(),
	)
	r.UseRawPath = true

	r.POST("/api/:id", func(c *gin.Context) {
		switch id := c.Param("id"); id {
		case "exists":
			c.String(http.StatusOK, id)
		case "panic":
			panic("oh noes!")
		case "httppanic":
			panic(http.ErrAbortHandler)
		default:
			c.Status(http.StatusNotFound)
		}
	})

	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server",
		Addr:    "localhost:0",
		Handler: r,
	})
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return srv.Serve(ctx)
	})
	t.Cleanup(func() {
		assert.Check(t, g.Wait())
	})

	client := hc.New(hc.Config{
		Name:    "test-client",
		BaseURL: "http://" + srv.Addr(),
	})

	t.Run("Hit an ID that exists", func(t *testing.T) {
		err = client.Call(ctx, hc.NewRequest("POST", "/api/%s",
			hc.RouteParams("exists"),
			hc.ResponseHeader(func(hdr http.Header) {
				assert.Check(t, cmp.Equal(hdr.Get("X-Route"), "/api/:id"))
			}),
		))
		assert.Assert(t, err)
	})

	t.Run("Hit an ID that does not exist", func(t *testing.T) {
		err = client.Call(ctx, hc.NewRequest("POST", "/api/%s",
			hc.RouteParams("does-not-exists"),
		))
		assert.Check(t, hc.HasStatusCode(err, http.StatusNotFound))
	})

	t.Run("Hit an ID that panics", func(t *testing.T) {
		resp, err := http.Post("http://"+srv.Addr()+"/api/panic", "", nil)
		assert.Assert(t, err)
		_ = resp.Body.Close()
		assert.Check(t, cmp.Equal(resp.StatusCode, http.StatusInternalServerError))
	})

	t.Run("Hit an ID that panics but does not rollbar", func(t *testing.T) {
		resp, err := http.Post("http://"+srv.Addr()+"/api/httppanic", "", nil)
		assert.Assert(t, err)
		_ = resp.Body.Close()
		assert.Check(t, cmp.Equal(resp.StatusCode, http.StatusInternalServerError))
	})
}

func TestMiddleware_Golden(t *testing.T) {
	start := time.Now()
	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Dataset:         "local-testing",
		GrpcHostAndPort: "127.0.0.1:4317",
		Service:         "app-main-golden-route",
		Version:         "dev-test",
	})
	provider := o11y.FromContext(ctx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := gin.New()
	r.Use(
		Middleware(provider, "test-server", nil),
		Recovery(),
	)
	r.UseRawPath = true

	r.POST("/api/:id", Golden(provider), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	srv := httptest.NewServer(r)

	client := hc.New(hc.Config{
		Name:    "test-client",
		BaseURL: srv.URL,
		Timeout: time.Second,
	})

	t.Run("call", func(t *testing.T) {
		err = client.Call(ctx, hc.NewRequest("POST", "/api/%s",
			hc.RouteParams("exists"),
			hc.ResponseHeader(func(hdr http.Header) {
				assert.Check(t, cmp.Equal(hdr.Get("X-Route"), "/api/:id"))
			}),
		))
		assert.Assert(t, err)
	})

	closeProvider(ctx)
	srv.Close()
	t.Run("check_spans", func(t *testing.T) {
		ctx := testcontext.Background()
		jc := jaeger.New("http://localhost:16686", "app-main-golden-route")

		// one joined up trace
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			traces, err := jc.Traces(ctx, start)
			if err != nil {
				return poll.Error(err)
			}
			if len(traces) >= 1 {
				return poll.Success()
			}
			return poll.Continue("only got %d traces", len(traces))
		})
		traces, err := jc.Traces(ctx, start)
		assert.NilError(t, err)
		// Now 2 traces golden and normal
		assert.Check(t, cmp.Len(traces, 2))

		// work out which trace is gold vs normal
		goldenTrace := traces[0]
		normalTrace := traces[1]
		if len(goldenTrace.Spans) == 2 {
			goldenTrace = traces[1]
			normalTrace = traces[0]
		}

		t.Run("normal", func(t *testing.T) {
			assert.Check(t, cmp.Len(normalTrace.Spans, 2))
			spans := normalTrace.Spans
			sort.Slice(spans, func(i, j int) bool { return spans[i].OperationName < spans[j].OperationName })
			assert.Check(t, cmp.Equal(spans[0].OperationName, "http-server test-server: POST /api/:id"))
			jaeger.AssertTag(t, spans[0].Tags, "span.kind", "server")
			assert.Check(t, cmp.Equal(spans[1].OperationName, "httpclient: test-client /api/%s"))
			jaeger.AssertTag(t, spans[1].Tags, "span.kind", "client")
		})

		t.Run("golden", func(t *testing.T) {
			// we did not mark the client span as golden so we should only expect one
			assert.Check(t, cmp.Len(goldenTrace.Spans, 1))
			spans := goldenTrace.Spans
			assert.Check(t, cmp.Equal(spans[0].OperationName, "http-server test-server: POST /api/:id"))
			jaeger.AssertTag(t, spans[0].Tags, "span.kind", "server")
			jaeger.AssertTag(t, spans[0].Tags, "meta.golden", "true")
		})
	})
}

func TestClientCancelled(t *testing.T) {
	m := &fakemetrics.Provider{}

	var b syncbuffer.SyncBuffer
	w := io.MultiWriter(os.Stdout, &b)
	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: m,
		Writer:  w,
	}))

	r := gin.New()
	r.Use(
		Middleware(o11y.FromContext(ctx), "test-server", nil),
		Recovery(),
		ClientCancelled(),
	)
	r.UseRawPath = true

	r.GET("/", func(c *gin.Context) {
		c.Status(200)
	})
	r.GET("/sleep", func(c *gin.Context) {
		ctx := c.Request.Context()
		t := time.NewTimer(10 * time.Second)
		defer t.Stop()
		select {
		case <-t.C:
			c.Status(200)
		case <-ctx.Done():
			c.JSON(500, gin.H{})
		}
	})

	server := httptest.NewServer(r)
	defer server.Close()

	client := hc.New(hc.Config{
		Name:    "test",
		BaseURL: server.URL,
		Timeout: 10 * time.Millisecond,
	})

	t.Run("success", func(t *testing.T) {
		b.Reset()
		m.Reset()
		req := hc.NewRequest("GET", "/")
		assert.Assert(t, client.Call(ctx, req))
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if !strings.Contains(b.String(), "http.status_code=200") {
				return poll.Continue("expected status not found")
			}
			return poll.Success()
		})

		assert.Check(t, cmp.DeepEqual([]fakemetrics.MetricCall{
			{
				Metric: "timer",
				Name:   "handler",
				Value:  0.111656,
				Tags: []string{
					"http.server_name:test-server", "http.method:GET", "http.route:/",
					"http.status_code:200",
				},
				Rate: 1,
			},
			{
				Metric: "timer",
				Name:   "httpclient",
				Value:  1.032934,
				Tags: []string{
					"http.client_name:test",
					"http.route:/",
					"http.method:GET",
					"http.status_code:200",
					"http.retry:false",
				},
				Rate: 1,
			},
		}, m.Calls(), fakemetrics.CMPMetrics))
	})

	t.Run("cancel", func(t *testing.T) {
		b.Reset()
		m.Reset()
		req := hc.NewRequest("GET", "/sleep", hc.Timeout(100*time.Millisecond))
		err := client.Call(ctx, req)
		assert.Check(t, cmp.ErrorIs(err, context.DeadlineExceeded))
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if !strings.Contains(b.String(), "http.status_code=499") {
				return poll.Continue("expected status not found")
			}
			return poll.Success()
		})

		assert.Check(t, cmp.DeepEqual([]fakemetrics.MetricCall{
			{
				Metric:   "count",
				Name:     "warning",
				ValueInt: 1,
				Tags:     []string{"type:o11y"},
				Rate:     1,
			},
			{
				Metric: "timer",
				Name:   "handler",
				Value:  100.344581,
				Tags: []string{
					"http.server_name:test-server",
					"http.method:GET",
					"http.route:/sleep",
					"http.status_code:499",
				},
				Rate: 1,
			},
		}, m.Calls(), fakemetrics.CMPMetrics))
	})
}

func TestRenderError(t *testing.T) {
	m := &fakemetrics.Provider{}

	buf := bytes.Buffer{}
	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: m,
		Writer:  &buf,
	}))

	r := gin.New()
	r.Use(
		Middleware(o11y.FromContext(ctx), "test-server", nil),
		ClientCancelled(),
	)
	r.UseRawPath = true

	r.GET("/", func(c *gin.Context) {
		c.Render(200, errorRenderer{})
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := hc.New(hc.Config{
		Name:    "test",
		BaseURL: server.URL,
		Timeout: 10 * time.Millisecond,
	})

	req := hc.NewRequest("GET", "/")
	assert.Check(t, client.Call(ctx, req))

	// check that the middleware added an error field
	assert.Check(t, cmp.Contains(buf.String(), "writer failure"))
	assert.Check(t, cmp.Contains(buf.String(), "app.gin_internal_error"))
}

type errorRenderer struct{}

func (e errorRenderer) Render(_ http.ResponseWriter) error {
	return errors.New("writer failure")
}

func (e errorRenderer) WriteContentType(_ http.ResponseWriter) {}
