package otel

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	o11yconf "github.com/circleci/ex/config/o11y"
	hc "github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/testcontext"
)

func TestO11y(t *testing.T) {
	start := time.Now()
	s := fakestatsd.New(t)
	ctx := testcontext.Background()
	t.Run("trace", func(t *testing.T) {
		o, err := New(Config{
			Config: o11yconf.Config{
				Service:        "app-main",
				Version:        "dev-test",
				Statsd:         s.Addr(),
				StatsNamespace: "test-app",
			},
			OtelDataset:     "local-testing",
			GrpcHostAndPort: "127.0.0.1:4317",
		})
		assert.NilError(t, err)

		// need to close the provider to be sure traces flushed
		defer o.Close(ctx)

		ctx = o11y.WithProvider(ctx, o)

		ctx, span := o.StartSpan(ctx, "root")
		defer span.End()

		doSomething(ctx)

		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(s.Metrics()) > 2 {
				return poll.Success()
			}
			return poll.Continue("not enough metrics yet")
		})

		gs := o.GetSpan(ctx)
		gs.AddRawField("raw_got", "13")
	})

	jc := newJaegerClient("http://localhost:16686", "app-main")
	traces, err := jc.Traces(ctx, start)
	assert.NilError(t, err)

	assert.Assert(t, cmp.Len(traces, 1))

	spanNames := map[string]bool{}
	for _, s := range traces[0].Spans {
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

func doSomething(ctx context.Context) {
	ctx, span := o11y.StartSpan(ctx, "operation")
	defer span.End()

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

func (p wrappedProvider) RawProvider() *OTel {
	return p.Provider.(*OTel)
}

func TestHelpers(t *testing.T) {
	op, err := New(Config{
		Config: o11yconf.Config{
			Service: "test-service",
			Mode:    "test",
			Version: "dev",
		},
		GrpcHostAndPort: "127.0.0.1:4317",
	})

	runTest := func(t *testing.T, provider o11y.Provider) {
		h := provider.Helpers()
		assert.Check(t, h != nil)

		ctx := o11y.WithProvider(context.Background(), provider)
		defer provider.Close(ctx)

		doSomething(ctx)

		t.Run("ids", func(t *testing.T) {
			traceID, parentID := h.TraceIDs(ctx)
			assert.Check(t, cmp.Equal(len(traceID), 32))
			assert.Check(t, cmp.Equal(len(parentID), 0))
		})

		t.Run("extract and inject", func(t *testing.T) {
			ctx, span := o11y.StartSpan(ctx, "test")
			defer span.End()

			svc1Propagation := h.ExtractPropagation(ctx)

			// Make a new context for a second "service"
			service2Context := o11y.WithProvider(context.Background(), provider)

			// Confirm it has not got a context
			svc2Propagation := h.ExtractPropagation(service2Context)
			assert.Check(t, cmp.Equal(len(svc2Propagation.Headers), 0))

			// Inject the propagation stuff into the new context
			service2Context, svc2Span := h.InjectPropagation(service2Context, svc1Propagation)
			defer svc2Span.End()

			// make sure the propagations match
			svc2Propagation = h.ExtractPropagation(service2Context)
			assert.Check(t, cmp.DeepEqual(svc1Propagation, svc2Propagation))

			// and make sure the two contexts have the same tracID
			traceID1, _ := h.TraceIDs(ctx)
			traceID2, _ := h.TraceIDs(service2Context)
			assert.Check(t, cmp.Equal(traceID1, traceID2))
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

func newJaegerClient(base, service string) jaegerClient {
	return jaegerClient{
		client: hc.New(hc.Config{
			Name:    "jaeger-api",
			BaseURL: base + "/api",
		}),
		service: service,
	}
}

type jaegerClient struct {
	client  *hc.Client
	service string
}

type jSpan struct {
	TraceID       string `json:"traceID"`
	SpanID        string `json:"spanID"`
	OperationName string `json:"operationName"`
	References    []struct {
		RefType string `json:"refType"`
		TraceID string `json:"traceID"`
		SpanID  string `json:"spanID"`
	} `json:"references"`
	StartTime int64 `json:"startTime"`
	Duration  int   `json:"duration"`
	Tags      []struct {
		Key   string      `json:"key"`
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	} `json:"tags"`
	Logs      []interface{} `json:"logs"`
	ProcessID string        `json:"processID"`
	Warnings  interface{}   `json:"warnings"`
}

type jTrace struct {
	ID    string  `json:"id"`
	Spans []jSpan `json:"spans"`
}

func (j *jaegerClient) Traces(ctx context.Context, since time.Time) ([]jTrace, error) {
	resp := struct {
		Data []jTrace `json:"data"`
	}{}
	err := j.client.Call(ctx, hc.NewRequest("GET", "/traces",
		hc.QueryParam("service", j.service),
		hc.QueryParam("start", strconv.FormatInt(since.UnixMicro(), 10)),
		hc.JSONDecoder(&resp),
	))
	return resp.Data, err
}

func TestRealCollector_HoneycombDatasetHeader(t *testing.T) {
	col := &testTraceCollector{}

	lis, err := net.Listen("tcp", "localhost:0")
	assert.Assert(t, err)

	grpcServer := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(grpcServer, col)

	g := &errgroup.Group{}
	g.Go(func() error {
		return grpcServer.Serve(lis)
	})

	t.Run("trace", func(t *testing.T) {
		prov, err := New(Config{
			Config:          o11yconf.Config{},
			OtelDataset:     "execyooshun",
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

		assert.Check(t, cmp.Equal(col.Spans()[0].Name, "roobar"))
		assert.Check(t, cmp.Equal(col.Metadata("x-honeycomb-dataset")[0], "execyooshun"))
	})
}

type testTraceCollector struct {
	coltracepb.UnimplementedTraceServiceServer

	// mutable state below here
	mu       sync.RWMutex
	metadata map[string][]string
	spans    []CollectSpan
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

func (c *testTraceCollector) Export(ctx context.Context,
	req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {

	c.mu.Lock()
	defer c.mu.Unlock()

	// last metadata in wins
	c.metadata, _ = metadata.FromIncomingContext(ctx)

	for _, resourceSpans := range req.GetResourceSpans() {
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				cspan := CollectSpan{
					Name:  span.GetName(),
					Code:  span.GetStatus().GetCode().String(),
					Desc:  span.GetStatus().GetMessage(),
					Attrs: map[string]string{},
				}
				for _, kv := range span.GetAttributes() {
					cspan.Attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
				}
				for _, event := range span.GetEvents() {
					cevent := CollectEvent{Name: event.GetName(), Attrs: map[string]string{}}
					for _, kv := range event.GetAttributes() {
						cevent.Attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
					}
					cspan.Events = append(cspan.Events, cevent)
				}
				c.spans = append(c.spans, cspan)
			}
		}
	}

	return &coltracepb.ExportTraceServiceResponse{}, nil
}
