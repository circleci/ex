package otel_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

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
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/jaeger"
	"github.com/circleci/ex/testing/testcontext"
)

func TestO11y(t *testing.T) {
	start := time.Now()
	s := fakestatsd.New(t)
	ctx := testcontext.Background()
	uuid := uuid.NewString()
	t.Run("trace", func(t *testing.T) {
		ctx, closeProvider, err := o11yconfig.Otel(ctx, o11yconfig.OtelConfig{
			Dataset:         "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
			Service:         "app-main",
			Version:         "dev-test",
			Statsd:          s.Addr(),
			StatsNamespace:  "test-app",
		})

		o := o11y.FromContext(ctx)
		assert.NilError(t, err)
		o.AddGlobalField("a_global_key", "a-global-value")

		// need to close the provider to be sure traces flushed
		defer closeProvider(ctx)

		ctx, span := o.StartSpan(ctx, "root")
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

	jc := jaeger.New("http://localhost:16686", "app-main")
	traces, err := jc.Traces(ctx, start)
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(traces, 1))

	spans := traces[0].Spans

	spanNames := map[string]bool{}
	for _, s := range spans {
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
		Service:         "app-main",
		Version:         "dev-test",
		Dataset:         "execyooshun",
		GrpcHostAndPort: lis.Addr().String(),
		StatsNamespace:  "test-app",
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

		span.RecordMetric(o11y.Count("count-events", "events", nil, "good"))
		span.RecordMetric(o11y.Timing("sub-time", "lemons", "good"))

		ns := o11y.FromContext(ctx).GetSpan(ctx)
		ns.AddField("frCtx", true)
		ns.AddRawField("raw", 34)
		ns.RecordMetric(o11y.Gauge("raw_gauge", "raw", "frCtx"))
	}(ctx)
}

type wrappedProvider struct {
	o11y.Provider
}

func (p wrappedProvider) RawProvider() *otel.Provider {
	return p.Provider.(*otel.Provider)
}

func TestHelpers(t *testing.T) {
	op, err := otel.New(otel.Config{
		GrpcHostAndPort: "127.0.0.1:4317",
	})

	runTest := func(t *testing.T, provider o11y.Provider) {
		h := provider.Helpers()
		assert.Check(t, h != nil)

		ctx := o11y.WithProvider(context.Background(), provider)
		defer provider.Close(ctx)

		doSomething(ctx, false)

		t.Run("ids", func(t *testing.T) {
			traceID, parentID := h.TraceIDs(ctx)
			assert.Check(t, cmp.Equal(len(traceID), 32))
			assert.Check(t, cmp.Equal(len(parentID), 0))
		})

		t.Run("extract and inject", func(t *testing.T) {
			ctx, span := o11y.StartSpan(ctx, "test")
			defer span.End()
			ctx = o11y.WithBaggage(ctx, o11y.Baggage{
				"bg1": "bgv1",
				"bg2": "bgv2",
			})

			svc1Propagation := h.ExtractPropagation(ctx)

			// Make a new context for a second "service"
			service2Context := o11y.WithProvider(context.Background(), provider)

			// Confirm it has not got a context
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
		})
	}

	t.Run("raw", func(t *testing.T) {
		runTest(t, op)
	})

	t.Run("wrapped", func(t *testing.T) {
		runTest(t, wrappedProvider{Provider: op})
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

		// Headers set on the grpc exporter become collector metadata.
		// N.B. This is not currently expected by the otel collectors.
		assert.Check(t, cmp.Contains(col.Metadata("x-honeycomb-dataset"), "execyooshun"))
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
	return parts[1]
}
