package otel_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	v1 "go.opentelemetry.io/proto/otlp/common/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	o11yconfig "github.com/circleci/ex/config/o11y"
	hc "github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/internal/syncbuffer"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/httprecorder/ginrecorder"
	"github.com/circleci/ex/testing/jaeger"
	"github.com/circleci/ex/testing/testcontext"
)

func TestO11y(t *testing.T) {
	tests := []struct {
		name string
		cfg  o11yconfig.OtelConfig
	}{
		{
			name: "grpc",
			cfg: o11yconfig.OtelConfig{
				Dataset:         "local-testing",
				GrpcHostAndPort: "127.0.0.1:4317",
				Service:         "grpc-main",
				Version:         "dev-test",
				StatsNamespace:  "test-app",
			},
		},
		{
			name: "http",
			cfg: o11yconfig.OtelConfig{
				Dataset:        "local-testing",
				HTTPTracesURL:  "http://127.0.0.1:4318",
				Service:        "http-main",
				Version:        "dev-test",
				StatsNamespace: "test-app",
			},
		},
		{
			name: "http with path",
			cfg: o11yconfig.OtelConfig{
				Dataset:        "local-testing",
				HTTPTracesURL:  "http://127.0.0.1:4318/v1/traces",
				Service:        "http-path-main",
				Version:        "dev-test",
				StatsNamespace: "test-app",
			},
		},
		{
			// We can't assert HTTPAuthorization is sent with the jaeger set up. TestO11y_Auth tests the token is sent
			name: "http with token",
			cfg: o11yconfig.OtelConfig{
				Dataset:           "local-testing",
				HTTPTracesURL:     "http://127.0.0.1:4318",
				Service:           "http-token-main",
				HTTPAuthorization: "my-token",
				Version:           "dev-test",
				StatsNamespace:    "test-app",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			s := fakestatsd.New(t)
			ctx := testcontext.Background()
			uuid := uuid.NewString()
			t.Run("send trace", func(t *testing.T) {
				cfg := tt.cfg
				cfg.Statsd = s.Addr()

				ctx, closeProvider, err := o11yconfig.Otel(ctx, cfg)

				o := o11y.FromContext(ctx)
				assert.NilError(t, err)
				o.AddGlobalField("a_global_key", "a-global-value")

				// need to close the provider to be sure traces flushed
				defer closeProvider(ctx)

				ctx, span := o.StartSpan(ctx, "root", o11y.WithSpanKind(o11y.SpanKindClient))
				defer span.End()

				doSomething(ctx, false)

				poll.WaitOn(t, func(t poll.LogT) poll.Result {
					if len(s.Metrics()) > 2 {
						return poll.Success()
					}
					return poll.Continue("not enough metrics yet")
				})

				gs := o.GetSpan(ctx)
				gs.AddRawField("raw_got", uuid)
				gs.AddField("test_app_fld", true)
				o11y.AddField(ctx, "test_app_o11y_fld", 42)
			})

			t.Run("find trace", func(t *testing.T) {

				jc := jaeger.New("http://localhost:16686", tt.cfg.Service)
				var traces []jaeger.Trace
				poll.WaitOn(t, func(t poll.LogT) poll.Result {
					var err error
					traces, err = jc.Traces(ctx, start)
					if err != nil {
						return poll.Error(err)
					}
					if len(traces) >= 1 {
						return poll.Success()
					}
					return poll.Continue("only got %d traces", len(traces))
				})
				assert.Assert(t, cmp.Len(traces, 1))

				spans := traces[0].Spans

				spanNames := map[string]bool{}
				for _, s := range spans {
					// all spans should have the trace level field (even though it was added in the context of a child span)
					jaeger.AssertTag(t, s.Tags, "app.trace_field", "trace_value")

					if s.OperationName == "root" {
						jaeger.AssertTag(t, s.Tags, "raw_got", uuid)
						jaeger.AssertTag(t, s.Tags, "a_global_key", "a-global-value")
						jaeger.AssertTag(t, s.Tags, "app.test_app_fld", "true")
						jaeger.AssertTag(t, s.Tags, "app.test_app_o11y_fld", "42")
					}

					// Jaeger passes otel resource span attributes into their process tags.
					jaeger.AssertTag(t, traces[0].Processes[s.ProcessID].Tags, "x-honeycomb-dataset", "local-testing")

					spanNames[s.OperationName] = true
				}
				assert.Check(t, cmp.DeepEqual(spanNames,
					map[string]bool{
						"root":          true,
						"operation":     true,
						"sub operation": true,
					},
				))
			})
		})
	}
}

func TestO11y_Auth(t *testing.T) {
	ctx := testcontext.Background()
	r := httprecorder.New()
	srv := httptest.NewServer(newOtelCollector(r))
	t.Cleanup(srv.Close)

	cfg := o11yconfig.OtelConfig{
		Dataset:           "local-testing",
		HTTPTracesURL:     srv.URL,
		Service:           "http-token-main",
		HTTPAuthorization: "my-token",
		Version:           "dev-test",
		StatsNamespace:    "test-app",
	}
	ctx, closeProvider, err := o11yconfig.Otel(ctx, cfg)
	assert.NilError(t, err)

	o := o11y.FromContext(ctx)
	ctx, span := o.StartSpan(ctx, "span")
	span.End()
	closeProvider(ctx) // force a flush

	assert.Check(t, cmp.Equal(r.LastRequest().Header.Get("Authorization"), "Bearer my-token"))
	ua := r.LastRequest().Header.Get("User-Agent")
	assert.Check(t, cmp.Contains(ua, "http-token-main"))
	assert.Check(t, cmp.Contains(ua, "dev-test"))
}

func TestProvider(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	assert.Assert(t, err)

	col := &testTraceCollector{}
	grpcServer := grpc.NewServer()
	defer grpcServer.Stop()
	coltracepb.RegisterTraceServiceServer(grpcServer, col)

	g := &errgroup.Group{}
	g.Go(func() error {
		return grpcServer.Serve(lis)
	})
	defer grpcServer.Stop()

	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Service:             "app-main",
		Version:             "dev-test",
		Dataset:             "execyooshun",
		GrpcHostAndPort:     lis.Addr().String(),
		StatsNamespace:      "test-app",
		DisableTailSampling: true,
	})
	assert.NilError(t, err)
	defer closeProvider(ctx)

	t.Run("no_span", func(t *testing.T) {
		// No span has been created yet so this should fail gracefully
		o11y.AddField(ctx, "o_key", "o_val")
	})

	t.Run("span", func(t *testing.T) {
		ctx, span := o11y.StartSpan(ctx, "a span")
		defer span.End()

		span.AddField("fld", 20)
		o11y.AddField(ctx, "p_key", "p_val")

		t.Run("context_key_conflict", func(t *testing.T) {
			// If the context uses this key form this will conflict and the span will not be available
			var key = struct{}{}
			ctx = context.WithValue(ctx, key, "")

			o11y.AddField(ctx, "cc_key", "cc_val")
		})
	})

	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		if len(col.Spans()) > 0 {
			return poll.Success()
		}
		return poll.Continue("spans never turned up")
	})

	assert.Check(t, cmp.Equal(len(col.spans), 1))
	assert.Check(t, cmp.Equal(col.spans[0].Attrs["app.cc_key"], "cc_val"))
	assert.Check(t, cmp.Equal(col.spans[0].Attrs["app.p_key"], "p_val"))
	assert.Check(t, cmp.Equal(col.resourceAttr["meta.sampling.disabled"], "true"))
}

func TestConcurrentSpanAccess(t *testing.T) {
	s := fakestatsd.New(t)
	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Service:        "app-main",
		Version:        "dev-test",
		Statsd:         s.Addr(),
		StatsNamespace: "sns",
	})
	assert.NilError(t, err)
	defer closeProvider(ctx)

	t.Run("writes", func(t *testing.T) {
		_, span := o11y.StartSpan(ctx, "a span")
		defer span.End()

		g := &errgroup.Group{}
		for n := 0; n < 1000; n++ {
			n := n
			g.Go(func() error {
				span.AddField(fmt.Sprintf("n_%d", n), "foo")
				return nil
			})
		}
		assert.Check(t, g.Wait())
	})

	t.Run("writes_and_ends", func(t *testing.T) {
		g := &errgroup.Group{}
		for n := 0; n < 1000; n++ {
			n := n
			_, span := o11y.StartSpan(ctx, "a span")
			g.Go(func() error {
				span.AddField(fmt.Sprintf("n_%d", n), "foo")
				return nil
			})
			g.Go(func() error {
				span.End()
				return nil
			})
		}
		assert.Check(t, g.Wait())
	})
}

func TestFailureMetrics(t *testing.T) {
	s := fakestatsd.New(t)
	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Service:        "app-main",
		Version:        "dev-test",
		Statsd:         s.Addr(),
		StatsNamespace: "sns",
	})
	assert.NilError(t, err)
	defer closeProvider(ctx)

	t.Run("non_nil_error", func(t *testing.T) {
		_, span := o11y.StartSpan(ctx, "a span")

		span.AddField("noteworthy_error", errors.New("something went wrong"))
		span.AddRawField("error", "some error")
		span.AddRawField("warning", "some warning")

		span.End()

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(s.Metrics()) > 2 {
				return poll.Success()
			}
			return poll.Continue("not enough metrics yet")
		})

		assert.Check(t, cmp.Equal(len(s.Metrics()), 3))

		found := 0
		wanted := []string{
			"sns.warning",
			"sns.failure",
			"sns.error",
		}
		for _, mn := range wanted {
			for _, m := range s.Metrics() {
				if m.Name == mn {
					found++
					continue
				}
			}
		}
		assert.Check(t, cmp.Equal(found, len(wanted)))
	})

	s.Reset()

	t.Run("nil_error", func(t *testing.T) {
		_, span := o11y.StartSpan(ctx, "a span")
		var ne error
		span.AddField("nil_error", ne)
		span.AddRawField("warning", "some warning") // just so we know when metrics were produced
		span.End()

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(s.Metrics()) > 0 {
				return poll.Success()
			}
			return poll.Continue("no metrics yet")
		})

		assert.Check(t, cmp.Equal(len(s.Metrics()), 1))
	})
}

func doSomething(ctx context.Context, flatten bool) {
	ctx, span := o11y.StartSpan(ctx, "operation")
	defer span.End()
	if flatten {
		span.Flatten("opp")
	}

	span.AddField("another_key", "yes")

	func(ctx context.Context) {
		ctx, span := o11y.StartSpan(ctx, "sub operation")
		defer span.End()

		span.AddField("lemons", "five")
		span.AddField("good", true)
		span.AddField("events", 22)
		o11y.FromContext(ctx).AddFieldToTrace(ctx, "trace_field", "trace_value")

		span.RecordMetric(o11y.Count("count-events", "events", nil, "good"))
		span.RecordMetric(o11y.Timing("sub-time", "lemons", "good"))

		ns := o11y.FromContext(ctx).GetSpan(ctx)
		ns.AddField("frCtx", true)
		ns.AddRawField("raw", 34)
		ns.RecordMetric(o11y.Gauge("raw_gauge", "raw", "frCtx"))
	}(ctx)
}

func TestHelpers(t *testing.T) {
	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Dataset:         "local-testing",
		GrpcHostAndPort: "127.0.0.1:4317",
		Service:         "app-main",
		Version:         "dev-test",
	})
	assert.NilError(t, err)
	provider := o11y.FromContext(ctx)

	start := time.Now()
	h := provider.Helpers()
	assert.Check(t, h != nil)

	doSomething(ctx, false)

	t.Run("ids", func(t *testing.T) {
		traceID, parentID := h.TraceIDs(ctx)
		assert.Check(t, cmp.Equal(len(traceID), 32))
		assert.Check(t, cmp.Equal(len(parentID), 0))
	})

	t.Run("extract and inject", func(t *testing.T) {
		ctx, span := o11y.StartSpan(ctx, "test")
		ctx = o11y.WithBaggage(ctx, o11y.Baggage{
			"bg1": "bgv1",
			"bg2": "bgv2",
		})

		ctx, gsp := o11y.StartSpan(ctx, "important")
		ctx = provider.MakeSpanGolden(ctx)
		gsp.End()

		svc1Propagation := h.ExtractPropagation(ctx)

		// Make a new context for a second "service"
		service2Context := o11y.WithProvider(context.Background(), provider)

		// Confirm it has not extracted any headers yet
		svc2Propagation := h.ExtractPropagation(service2Context)
		assert.Check(t, cmp.Equal(len(svc2Propagation.Headers), 0))

		// Inject the propagation stuff into the new context
		service2Context, svc2Span := h.InjectPropagation(service2Context, svc1Propagation)
		defer svc2Span.End()

		// and make sure the two contexts have the same tracID
		traceID1, _ := h.TraceIDs(ctx)
		traceID2, _ := h.TraceIDs(service2Context)
		assert.Check(t, cmp.Equal(traceID1, traceID2))

		// and that svc2 context has baggage
		b := o11y.GetBaggage(ctx)
		assert.Check(t, cmp.Equal(b["bg1"], "bgv1"))
		assert.Check(t, cmp.Equal(b["bg2"], "bgv2"))

		ctx, gsp2 := o11y.StartSpan(ctx, "important-2")
		ctx = provider.MakeSpanGolden(ctx)
		gsp2.End()

		span.End()

		closeProvider(ctx)
		t.Run("check", func(t *testing.T) {
			jc := jaeger.New("http://localhost:16686", "app-main")
			traces, err := jc.Traces(ctx, start)
			assert.NilError(t, err)

			// We should have two normal traces and one golden trace
			// if propagation of the golden trace is bad  - this will be 4
			assert.Assert(t, cmp.Len(traces, 3))
		})
	})

	assert.NilError(t, err)
}

func TestRealCollector_HoneycombDataset(t *testing.T) {
	col := &testTraceCollector{}

	lis, err := net.Listen("tcp", "localhost:0")
	assert.Assert(t, err)

	grpcServer := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(grpcServer, col)
	defer grpcServer.Stop()

	g := &errgroup.Group{}
	g.Go(func() error {
		return grpcServer.Serve(lis)
	})

	t.Run("trace", func(t *testing.T) {
		prov, err := otel.New(otel.Config{
			Dataset:         "execyooshun",
			GrpcHostAndPort: lis.Addr().String(),
		})
		assert.NilError(t, err)
		ctx := o11y.WithProvider(context.Background(), prov)
		ctx, span := prov.StartSpan(ctx, "roobar")
		span.End()

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(col.Spans()) > 0 {
				return poll.Success()
			}
			return poll.Continue("spans never turned up")
		})

		span0 := col.Spans()[0]
		assert.Check(t, cmp.Equal(span0.Name, "roobar"))

		// Check the resource attributes
		assert.Check(t, cmp.Equal(col.ResourceAttribute("x-honeycomb-dataset"), "execyooshun"))
	})
}

func TestSampling(t *testing.T) {
	col := &testTraceCollector{}

	lis, err := net.Listen("tcp", "localhost:0")
	assert.Assert(t, err)

	grpcServer := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(grpcServer, col)

	g := &errgroup.Group{}
	g.Go(func() error {
		return grpcServer.Serve(lis)
	})

	t.Run("by-name", func(t *testing.T) {
		prov, err := otel.New(otel.Config{
			Dataset:         "execyooshun",
			GrpcHostAndPort: lis.Addr().String(),
			SampleTraces:    true,
			SampleKeyFunc: func(m map[string]any) string {
				if _, ok := m["duration_ms"].(int); !ok {
					t.Error("duration_ms is not an int or does not exist")
				}
				return fmt.Sprintf("%s", m["name"])
			},
			SampleRates: map[string]uint{
				"span-name": 10, // 1 in 10 spans to be kept
			},
		})
		assert.NilError(t, err)
		ctx := o11y.WithProvider(context.Background(), prov)
		for n := 0; n < 100; n++ {
			_, span := prov.StartSpan(ctx, "span-name")
			span.AddField("number", n)
			span.End()
		}

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(col.Spans()) > 2 {
				return poll.Success()
			}
			return poll.Continue("not enough spans never turned up. Sampling too heavily?")
		})

		assert.Check(t, len(col.Spans()) < 30, "got too many spans: %d", len(col.Spans()))
		assert.Check(t, cmp.Equal(col.Spans()[0].Attrs["SampleRate"], "10"))
	})

	// n.b. don't only test with name - since that is available in the head sampler
	t.Run("by-attrib", func(t *testing.T) {
		prov, err := otel.New(otel.Config{
			Dataset:         "execyooshun",
			GrpcHostAndPort: lis.Addr().String(),
			SampleTraces:    true,
			SampleKeyFunc: func(m map[string]any) string {
				return fmt.Sprintf("%v", m["app.sample_thing"])
			},
			SampleRates: map[string]uint{
				"roobar": 10, // 1 in 10 spans to be kept
			},
		})
		assert.NilError(t, err)
		ctx := o11y.WithProvider(context.Background(), prov)
		for n := 0; n < 100; n++ {
			_, span := prov.StartSpan(ctx, "span-name")
			span.AddField("number", n)
			span.AddField("sample_thing", "roobar")
			span.End()
		}

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(col.Spans()) > 5 {
				return poll.Success()
			}
			return poll.Continue("not enough spans never turned up. Sampling too heavily?")
		})

		assert.Check(t, len(col.Spans()) < 30, "got too many spans: %d", len(col.Spans()))
	})
}

func TestKind(t *testing.T) {
	var (
		srvURL              string
		closeServerProvider func()
		closeClientProvider func()
		closeServer         func()
		start               = time.Now()
	)

	t.Run("server", func(t *testing.T) {
		ctx, clp, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
			Dataset:         "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main-server",
			Version:         "dev-test",
		})
		assert.NilError(t, err)
		closeServerProvider = func() {
			clp(ctx)
		}
		r := ginrouter.Default(ctx, "main-server")
		r.GET("/", func(c *gin.Context) {
			c.JSON(200, gin.H{})
		})
		srv := httptest.NewServer(r)
		srvURL = srv.URL
		closeServer = srv.Close
	})

	t.Run("client", func(t *testing.T) {
		ctx, clp, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
			Dataset:         "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main-client",
			Version:         "dev-test",
		})
		assert.NilError(t, err)
		closeClientProvider = func() {
			clp(ctx)
		}

		cl := hc.New(hc.Config{
			BaseURL:   srvURL,
			Name:      "test-client",
			UserAgent: "test-main",
		})
		assert.NilError(t, cl.Call(ctx, hc.NewRequest(http.MethodGet, "/")))
	})

	closeServerProvider()
	closeServer()
	closeClientProvider()

	t.Run("check_spans", func(t *testing.T) {
		ctx := testcontext.Background()
		jc := jaeger.New("http://localhost:16686", "app-main-server")

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
		assert.Check(t, cmp.Len(traces, 1))
		assert.Check(t, cmp.Len(traces[0].Spans, 2))
		spans := traces[0].Spans

		slices.SortFunc(spans, serverSpanFirst)
		sort.Slice(spans, func(i, j int) bool { return spans[i].OperationName < spans[j].OperationName })
		assert.Check(t, cmp.Equal(spans[0].OperationName, "GET /"))
		jaeger.AssertTag(t, spans[0].Tags, "span.kind", "server")
		assert.Check(t, cmp.Equal(spans[1].OperationName, "GET /"))
		jaeger.AssertTag(t, spans[1].Tags, "span.kind", "client")
	})
}

func serverSpanFirst(a, b jaeger.Span) int {
	if jaeger.HasTag(a.Tags, "span.kind", "server") && !jaeger.HasTag(b.Tags, "span.kind", "server") {
		return -1
	}
	if jaeger.HasTag(a.Tags, "span.kind", "server") && jaeger.HasTag(b.Tags, "span.kind", "server") {
		return 0
	}
	return 1
}

func TestFlatten(t *testing.T) {
	start := time.Now()
	statsd := fakestatsd.New(t)
	ctx := testcontext.Background()
	uuid := uuid.NewString()
	t.Run("trace", func(t *testing.T) {
		ctx, closeProvider, err := o11yconfig.Otel(ctx, o11yconfig.OtelConfig{
			Dataset:         "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main",
			Version:         "dev-test",
			Statsd:          statsd.Addr(),
			StatsNamespace:  "test-app",
		})

		o := o11y.FromContext(ctx)
		assert.NilError(t, err)
		o.AddGlobalField("a_global_key", "a-global-value")

		// need to close the provider to be sure traces flushed
		defer closeProvider(ctx)

		ctx, span := o.StartSpan(ctx, "root")
		defer span.End()

		doSomething(ctx, true)

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(statsd.Metrics()) > 2 {
				return poll.Success()
			}
			return poll.Continue("not enough metrics yet")
		})

		gs := o.GetSpan(ctx)
		gs.AddRawField("raw_got", uuid)
		gs.AddField("test_app_fld", true)
		o11y.AddField(ctx, "test_app_o11y_fld", 42)
	})

	jc := jaeger.New("http://localhost:16686", "app-main")
	traces, err := jc.Traces(ctx, start)
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(traces, 1))

	spans := traces[0].Spans
	// only one span
	assert.Check(t, cmp.Equal(len(spans), 1))

	s := spans[0]
	assert.Check(t, cmp.Equal(s.OperationName, "root"))
	jaeger.AssertTag(t, s.Tags, "raw_got", uuid)
	jaeger.AssertTag(t, s.Tags, "a_global_key", "a-global-value")
	jaeger.AssertTag(t, s.Tags, "app.test_app_fld", "true")
	jaeger.AssertTag(t, s.Tags, "app.test_app_o11y_fld", "42")

	// check the child tags
	jaeger.AssertTag(t, s.Tags, "opp.app.another_key", "yes")
	jaeger.AssertTag(t, s.Tags, "opp.l2.app.lemons", "five")
	jaeger.AssertTag(t, s.Tags, "opp.l2.app.good", "true")
	jaeger.AssertTag(t, s.Tags, "opp.l2.app.events", "22")
}

func TestGolden(t *testing.T) {
	start := time.Now()
	s := fakestatsd.New(t)
	ctx := testcontext.Background()

	t.Run("trace", func(t *testing.T) {
		ctx, closeProvider, err := o11yconfig.Otel(ctx, o11yconfig.OtelConfig{
			Dataset:         "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main-gold",
			Version:         "dev-test",
			Statsd:          s.Addr(),
			StatsNamespace:  "test-app",
		})

		t.Run("no span", func(t *testing.T) {
			o11y.MakeSpanGolden(ctx)
		})

		o := o11y.FromContext(ctx)
		assert.NilError(t, err)
		o.AddGlobalField("a_global_key", "a-global-value")

		t.Run("no span provider", func(t *testing.T) {
			o.MakeSpanGolden(ctx)
		})

		func() {
			ctx, span := o.StartSpan(ctx, "fun")
			time.Sleep(time.Millisecond)
			span.End()

			ctx, span = o.StartSpan(ctx, "fun2")
			time.Sleep(time.Millisecond)
			span.End()

			// need to close the provider to be sure traces flushed
			defer closeProvider(ctx)
			defer func() {
				time.Sleep(time.Millisecond * 2)

				ctx, span := o.StartSpan(ctx, "key-event")
				ctx = o.MakeSpanGolden(ctx)
				time.Sleep(time.Millisecond)
				span.End()
			}()

			ctx, span = o.StartSpan(ctx, "trigger-event", o11y.WithSpanKind(o11y.SpanKindServer))
			ctx = o.MakeSpanGolden(ctx)
			defer span.End()

			doSomething(ctx, false)

			poll.WaitOn(t, func(t poll.LogT) poll.Result {
				if len(s.Metrics()) > 2 {
					return poll.Success()
				}
				return poll.Continue("not enough metrics yet")
			})

			// inject a brand-new trace
			newCtx := context.Background()
			_, newSpan := o.StartSpan(newCtx, "new-trace")
			time.Sleep(time.Millisecond)
			newSpan.End()
		}()

		t.Run("check", func(t *testing.T) {
			jc := jaeger.New("http://localhost:16686", "app-main-gold")
			traces, err := jc.Traces(ctx, start)
			assert.NilError(t, err)

			// We should have two normal traces and one golden trace
			assert.Assert(t, cmp.Len(traces, 3))

			spans := make([]jaeger.Span, 0, 9)
			for _, trc := range traces {
				spans = append(spans, trc.Spans...)
			}
			assert.Check(t, cmp.Len(spans, 9))

			cnt := 0
			cntG := 0
			golden := []string{"trigger", "trigger-event", "key-event"}
			for _, sp := range spans {
				// check for the span kind
				if sp.OperationName == "trigger-event" {
					jaeger.AssertTag(t, sp.Tags, "span.kind", "server")
				}
				isGold := jaeger.HasTag(sp.Tags, "meta.golden", "true")
				if isGold {
					cntG++
					// Make sure a golden span has one of the expected span names
					expectedGoldName := false
					for _, g := range golden {
						if sp.OperationName == g {
							expectedGoldName = true
						}
					}
					assert.Check(t, expectedGoldName)
				}
				// count the spans with the golden names
				for _, g := range golden {
					if sp.OperationName == g {
						cnt++
					}
				}
			}

			// Check golden spans are duplicated as non-golden (except the start span)
			assert.Check(t, cmp.Equal(cnt, 4))
			assert.Check(t, cmp.Equal(cntG, 2))
		})
	})
}

func TestSpan(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	assert.Assert(t, err)

	col := &testTraceCollector{}
	grpcServer := grpc.NewServer()
	defer grpcServer.Stop()
	coltracepb.RegisterTraceServiceServer(grpcServer, col)

	g := &errgroup.Group{}
	g.Go(func() error {
		return grpcServer.Serve(lis)
	})
	defer grpcServer.Stop()

	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Service:         "app-main",
		Version:         "dev-test",
		Dataset:         "execyooshun",
		GrpcHostAndPort: lis.Addr().String(),
		StatsNamespace:  "test-app",
	})

	assert.NilError(t, err)

	ctx, span := o11y.StartSpan(ctx, "span", o11y.WithSpanKind(o11y.SpanKindClient))

	var (
		nilTime *time.Time
		nilInt  *int
		four    = 4
		tim     = time.Date(2020, 1, 1, 4, 5, 6, 7, time.UTC)
		aThing  = "a thing"
	)

	attrs := []struct {
		name     string
		val      any
		expected string
	}{
		{
			name:     "nil_time_pointer",
			val:      nilTime,
			expected: "",
		},
		{
			name:     "nil_int_pointer",
			val:      nilInt,
			expected: "",
		},
		{
			name:     "time_pointer",
			val:      &tim,
			expected: "2020-01-01 04:05:06.000000007 +0000 UTC",
		},
		{
			name:     "int_pointer",
			val:      &four,
			expected: "4",
		},
		{
			name:     "string_pointer",
			val:      &aThing,
			expected: "a thing",
		},
	}
	for _, tt := range attrs {
		span.AddField(tt.name, tt.val)
	}

	span.End()

	closeProvider(ctx)

	assert.Check(t, cmp.Equal(col.spans[0].Kind, "SPAN_KIND_CLIENT"))
	for _, tt := range attrs {
		assert.Check(t, cmp.Equal(tt.expected, col.spans[0].Attrs["app."+tt.name]), tt.name)
	}
}

type testTraceCollector struct {
	coltracepb.UnimplementedTraceServiceServer

	// mutable state below here
	mu sync.RWMutex
	// metadata is where headers set on the exporter headers will end up
	metadata map[string][]string
	// resource attributes are populated from string attributes set on the last received resource.
	resourceAttr map[string]string
	// spans are populated from the individual otel 'scope spans'.
	spans []CollectSpan
}

type CollectSpan struct {
	Name   string
	Code   string
	Desc   string
	Kind   string
	Attrs  map[string]string
	Events []CollectEvent
}

type CollectEvent struct {
	Name  string
	Attrs map[string]string
}

func (c *testTraceCollector) Spans() []CollectSpan {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spans := make([]CollectSpan, len(c.spans))
	copy(spans, c.spans)
	return spans
}

func (c *testTraceCollector) Metadata(what string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ma := c.metadata[what]
	if len(ma) == 0 {
		return nil
	}
	r := make([]string, len(ma))
	copy(r, ma)
	return r
}

func (c *testTraceCollector) ResourceAttribute(what string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.resourceAttr[what]
}

func (c *testTraceCollector) Export(ctx context.Context,
	req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	// last metadata in wins
	c.metadata, _ = metadata.FromIncomingContext(ctx)

	for _, resourceSpans := range req.GetResourceSpans() {
		r := resourceSpans.GetResource()
		if r != nil {
			c.resourceAttr = map[string]string{}
			for _, v := range r.GetAttributes() {
				c.resourceAttr[v.GetKey()] = toString(v.GetValue())
			}
		}
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				cspan := CollectSpan{
					Name:  span.GetName(),
					Code:  span.GetStatus().GetCode().String(),
					Desc:  span.GetStatus().GetMessage(),
					Kind:  span.GetKind().String(),
					Attrs: map[string]string{},
				}
				for _, kv := range span.GetAttributes() {
					cspan.Attrs[kv.GetKey()] = toString(kv.GetValue())
				}
				for _, event := range span.GetEvents() {
					ce := CollectEvent{Name: event.GetName(), Attrs: map[string]string{}}
					for _, kv := range event.GetAttributes() {
						ce.Attrs[kv.GetKey()] = toString(kv.GetValue())
					}
					cspan.Events = append(cspan.Events, ce)
				}
				c.spans = append(c.spans, cspan)
			}
		}
	}

	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func toString(value *v1.AnyValue) string {
	v := value.GetStringValue()
	if v != "" {
		return v
	}
	parts := strings.Split(value.String(), ":")
	return strings.Trim(parts[1], `"`)
}

func TestOtel_Writer(t *testing.T) {
	var b syncbuffer.SyncBuffer
	w := io.MultiWriter(os.Stdout, &b)
	op, err := otel.New(otel.Config{
		Writer: w,
	})
	assert.NilError(t, err)
	ctx := o11y.WithProvider(context.Background(), op)
	ctx, span := o11y.StartSpan(ctx, "a span")
	o11y.End(span, nil)
	op.Close(ctx)
	assert.Check(t, cmp.Contains(b.String(), "a span"))
}

func newOtelCollector(recorder *httprecorder.RequestRecorder) http.Handler {
	ctx := testcontext.Background()
	r := ginrouter.Default(ctx, "fake-otel-collector")
	r.Use(ginrecorder.Middleware(ctx, recorder))

	r.POST("/v1/traces", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func TestMetrics(t *testing.T) {
	ctx := context.Background()
	s := fakestatsd.New(t)
	ns := "test"
	ctx, closeProvider, err := o11yconfig.Otel(context.Background(), o11yconfig.OtelConfig{
		Service:        "app-main",
		Version:        "dev-test",
		Statsd:         s.Addr(),
		StatsNamespace: ns,
	})
	assert.NilError(t, err)
	defer closeProvider(ctx)

	_, span := o11y.StartSpan(ctx, "test")
	span.RecordMetric(o11y.Timing("timing"))

	span.AddField("count", 22)
	span.RecordMetric(o11y.Count("count", "count", nil))

	span.AddField("custom_duration", 10000)
	span.RecordMetric(o11y.Duration("duration", "custom_duration"))

	span.RecordMetric(o11y.Incr("counter"))

	span.AddRawField("gauge", 34)
	span.RecordMetric(o11y.Gauge("gauge", "gauge"))

	time.Sleep(100 * time.Millisecond)
	span.End()

	var metrics []fakestatsd.Metric
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		m := s.Metrics()
		if len(m) > 4 {
			metrics = m
			return poll.Success()
		}
		return poll.Continue("not enough metrics yet")
	})

	expected := []struct {
		name   string
		assert func(v string)
	}{
		{
			name: "timing",
			assert: func(v string) {
				assertDuration(t, v, 100*time.Millisecond)
			},
		},
		{
			name: "count",
			assert: func(v string) {
				assertInt(t, v, 22)
			},
		},
		{
			name: "duration",
			assert: func(v string) {
				assertDuration(t, v, 10000*time.Millisecond)
			},
		},
		{
			name: "counter",
			assert: func(v string) {
				assertInt(t, v, 1)
			},
		},
		{
			name: "gauge",
			assert: func(v string) {
				assertInt(t, v, 34)
			},
		},
	}
	for _, e := range expected {
		found := false
		for _, m := range metrics {
			if m.Name != ns+"."+e.name {
				continue
			}

			found = true
			valParts := strings.Split(m.Value, "|")
			assert.Check(t, len(valParts) >= 2)
			e.assert(valParts[0])
		}
		assert.Check(t, found, "no metric named %s", e.name)
	}
}

func assertDuration(t *testing.T, v string, expected time.Duration) {
	d, err := time.ParseDuration(v + "ms")
	assert.NilError(t, err)
	delta := d - expected
	assert.Check(t, delta >= 0)
}

func assertInt(t *testing.T, v string, expected int) {
	i, err := strconv.Atoi(v)
	assert.NilError(t, err)
	assert.Check(t, cmp.Equal(i, expected))
}
