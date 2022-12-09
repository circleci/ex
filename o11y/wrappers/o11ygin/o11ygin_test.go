package o11ygin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/testing/fakemetrics"
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

	client := httpclient.New(httpclient.Config{
		Name:    "test-client",
		BaseURL: "http://" + srv.Addr(),
	})

	t.Run("Hit an ID that exists", func(t *testing.T) {
		err = client.Call(ctx, httpclient.NewRequest("POST", "/api/%s",
			httpclient.RouteParams("exists"),
		))
		assert.Assert(t, err)
	})

	t.Run("Hit an ID that does not exist", func(t *testing.T) {
		err = client.Call(ctx, httpclient.NewRequest("POST", "/api/%s",
			httpclient.RouteParams("does-not-exists"),
		))
		assert.Check(t, httpclient.HasStatusCode(err, http.StatusNotFound))
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

func TestClientCancelled(t *testing.T) {
	m := &fakemetrics.Provider{}

	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: m,
	}))

	r := gin.New()
	r.Use(
		Middleware(o11y.FromContext(ctx), "test-server", nil),
		Recovery(),
	)
	r.UseRawPath = true

	r.Use(func(c *gin.Context) {
		c.Next()
		if c.Request.URL.Path == "/sleep" {
			assert.Check(t, c.Writer.Status() == 499)
		}
	})
	r.Use(ClientCancelled())

	r.GET("/", func(c *gin.Context) {
		c.Status(200)
	})
	r.GET("/sleep", func(c *gin.Context) {
		time.Sleep(time.Second)
		c.Status(200)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := httpclient.New(httpclient.Config{
		Name:    "test",
		BaseURL: server.URL,
		Timeout: 10 * time.Millisecond,
	})

	t.Run("success", func(t *testing.T) {
		req := httpclient.NewRequest("GET", "/")
		assert.Assert(t, client.Call(ctx, req))
	})

	t.Run("cancel", func(t *testing.T) {
		req := httpclient.NewRequest("GET", "/sleep", httpclient.Timeout(time.Millisecond))
		err := client.Call(ctx, req)
		assert.Check(t, cmp.ErrorIs(err, context.DeadlineExceeded))
	})
}
