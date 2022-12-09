package o11ynethttp

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/honeycombio/beeline-go/trace"
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
		t.Run("Check metrics", func(t *testing.T) {

			assert.Check(t, cmp.DeepEqual(
				[]fakemetrics.MetricCall{
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
						Value:  1,
						Tags: []string{
							"server_name:test-server",
							"request.method:POST",
							"request.route:unknown",
							"response.status_code:200",
						},
						Rate: 1,
					},
					{
						Metric: "timer",
						Name:   "httpclient",
						Value:  1,
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
						Value:  1,
						Tags: []string{
							"server_name:test-server",
							"request.method:POST",
							"request.route:unknown",
							"response.status_code:404",
						},
						Rate: 1,
					},
					{
						Metric: "timer",
						Name:   "httpclient",
						Value:  1,
						Tags: []string{
							"http.client_name:test-client",
							"http.route:/api/%s",
							"http.method:POST",
							"http.status_code:404",
							"http.retry:false",
						},
						Rate: 1,
					},
				},
				m.Calls(), fakemetrics.CMPMetrics, cmpopts.IgnoreFields(fakemetrics.MetricCall{}, "Value", "ValueInt")),
			)
		})
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := http.NewServeMux()
	r.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		trace.GetSpanFromContext(r.Context()).AddField("request.route", "/api/%s")
		switch r.URL.Path {
		case "/api/exists":
			_, _ = io.WriteString(w, "exists")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server",
		Addr:    "localhost:0",
		Handler: Middleware(provider, "test-server", r),
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
			httpclient.RouteParams("does-not-exist"),
		))
		assert.Check(t, httpclient.HasStatusCode(err, http.StatusNotFound))
	})
}

func TestMiddleware_with_sampling(t *testing.T) {
	m := &fakemetrics.Provider{}

	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:       "color",
		Metrics:      m,
		SampleTraces: true,
		SampleKeyFunc: func(m map[string]interface{}) string {
			return "foo"
		},
		SampleRates: map[string]int{
			"foo": 1e4,
		},
	}))
	provider := o11y.FromContext(ctx)
	t.Cleanup(func() {
		provider.Close(ctx)
		t.Run("Check metrics", func(t *testing.T) {

			assert.Check(t, cmp.DeepEqual(
				[]fakemetrics.MetricCall{
					{
						Metric: "count",
						Name:   "warning",
						Value:  1,
						Tags:   []string{"type:o11y"},
						Rate:   1,
					},
					{
						Metric: "timer",
						Name:   "handler",
						Value:  1,
						Tags: []string{
							"server_name:test-server",
							"request.method:POST",
							"request.route:/api/",
							"response.status_code:200",
						},
						Rate: 1,
					},
					{
						Metric: "timer",
						Name:   "httpclient",
						Value:  1,
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
						Value:  1,
						Tags: []string{
							"server_name:test-server",
							"request.method:POST",
							"request.route:/api/",
							"response.status_code:404",
						},
						Rate: 1,
					},
					{
						Metric: "timer",
						Name:   "httpclient",
						Value:  1,
						Tags: []string{
							"http.client_name:test-client",
							"http.route:/api/%s",
							"http.method:POST",
							"http.status_code:404",
							"http.retry:false",
						},
						Rate: 1,
					},
				},
				m.Calls(), fakemetrics.CMPMetrics, cmpopts.IgnoreFields(fakemetrics.MetricCall{}, "Value", "ValueInt")),
			)
		})
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := http.NewServeMux()
	r.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		GetRouteRecorderFromContext(r.Context()).SetRoute("/api/")

		switch r.URL.Path {
		case "/api/exists":
			_, _ = io.WriteString(w, "exists")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server",
		Addr:    "localhost:0",
		Handler: Middleware(provider, "test-server", r),
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
			httpclient.RouteParams("does-not-exist"),
		))
		assert.Check(t, httpclient.HasStatusCode(err, http.StatusNotFound))
	})
}
