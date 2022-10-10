package otel

import (
	"context"
	"strconv"
	"testing"
	"time"

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

func TestHelpers(t *testing.T) {
	provider, err := New(Config{
		Config: o11yconf.Config{
			Service: "test-service",
			Mode:    "test",
			Version: "dev",
		},
		GrpcHostAndPort: "127.0.0.1:4317",
	})
	assert.NilError(t, err)

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
