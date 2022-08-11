package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMetrics(t *testing.T) {

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 100)
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("starvation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(testcontext.Background())
		defer cancel()

		ctx, done := setupMetrics(t, ctx)
		tracer := New(ctx)

		sys := system.New()
		sys.AddGauges(tracer)
		go func() {
			err := sys.Run(ctx, time.Millisecond)
			assert.Assert(t, err)
		}()

		concurrentRequests := 100
		maxConnections := 90 // close to max concurrency, but we should see some waiting
		cl := httpclient.New(httpclient.Config{
			Name:                  "test-client",
			BaseURL:               s.URL,
			MaxConnectionsPerHost: maxConnections,
			Tracer:                tracer,
		})

		r := httpclient.NewRequest("GET", "/test/%s",
			httpclient.RouteParams("foo"),
			httpclient.Timeout(time.Second),
		)
		err := cl.Call(ctx, r)
		assert.Assert(t, err)

		r = httpclient.NewRequest("GET", "/test/%s",
			httpclient.RouteParams("foo"),
			httpclient.Timeout(time.Second),
		)
		err = cl.Call(ctx, r)
		assert.Assert(t, err)

		var wg sync.WaitGroup
		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test",
					httpclient.Timeout(time.Second),
				)
				err := cl.Call(ctx, r)
				assert.Assert(t, err)
			}()
		}
		wg.Wait()

		gauges := tracer.Gauges(ctx)
		g, ok := gauges["in_flight"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 2))
		assert.Check(t, cmp.Equal(g[0].Val, float64(0)))
		assert.Check(t, g[1].Val > float64(70), g[1].Val)
		assert.Check(t, g[1].Val <= float64(100), g[1].Val)

		g, ok = gauges["pool_avail_estimate"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 1))

		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test/thing",
					httpclient.Timeout(time.Second),
				)
				err := cl.Call(ctx, r)
				assert.Assert(t, err)
			}()
		}

		wg.Wait()

		cancel()
		metrics := done() // wait for the stats to be flushed

		assert.Check(t, len(metrics) > 900, len(metrics))
		assert.Check(t, len(metrics) < 1000, len(metrics))

		assertIn(t, metrics, "delayed:true", 5, 70)
		assertIn(t, metrics, "starved:true", 5, 70)
		idle := (concurrentRequests - maxConnections) + concurrentRequests
		assertIn(t, metrics, "idle:false", idle-20, idle+20)
		assertIn(t, metrics, "reused:false", maxConnections-60, maxConnections)
	})

	t.Run("no-starvation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(testcontext.Background())
		defer cancel()

		ctx, done := setupMetrics(t, ctx)
		tracer := New(ctx)

		sys := system.New()
		sys.AddGauges(tracer)
		go func() {
			err := sys.Run(ctx, time.Millisecond)
			assert.Assert(t, err)
		}()

		concurrentRequests := 20
		maxConnections := 70
		cl := httpclient.New(httpclient.Config{
			Name:                  "test-client",
			BaseURL:               s.URL,
			MaxConnectionsPerHost: maxConnections,
			Tracer:                tracer,
		})

		var wg sync.WaitGroup
		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test",
					httpclient.Timeout(time.Second),
				)
				err := cl.Call(ctx, r)
				assert.Assert(t, err)
			}()
		}
		wg.Wait()

		gauges := tracer.Gauges(ctx)
		g, ok := gauges["in_flight"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 2))
		assert.Check(t, cmp.Equal(g[0].Val, float64(0)))
		assert.Check(t, g[1].Val > float64(15), g[1].Val)
		assert.Check(t, g[1].Val <= float64(50), g[1].Val)
		g, ok = gauges["pool_avail_estimate"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 1))

		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test/thing",
					httpclient.Timeout(time.Second),
				)
				err := cl.Call(ctx, r)
				assert.Assert(t, err)
			}()
		}

		wg.Wait()

		cancel()
		metrics := done() // wait for the stats server to stop

		assert.Check(t, len(metrics) > 150, len(metrics))
		assert.Check(t, len(metrics) < 220, len(metrics))

		assertIn(t, metrics, "delayed:true", 0, 0)
		assertIn(t, metrics, "starved:true", 0, 0)
		assertIn(t, metrics, "idle:false", concurrentRequests, concurrentRequests)
		assertIn(t, metrics, "reused:false", concurrentRequests, concurrentRequests)
	})
}

func assertIn(t *testing.T, ls []fakestatsd.Metric, what string, min, max int) {
	t.Helper()
	count := 0

	for _, l := range ls {
		for _, tag := range l.Tags {
			if strings.Contains(tag, what) {
				count++
			}
		}
	}
	t.Logf("found %d matches", count)
	assert.Check(t, count >= min, "count:%d < min:%d", count, min)
	assert.Check(t, count <= max, "count:%d > max:%d", count, max)
}

func setupMetrics(t *testing.T, ctx context.Context) (context.Context, func() []fakestatsd.Metric) {
	s := fakestatsd.New(t)

	stats, err := statsd.New(s.Addr())
	assert.Assert(t, err)

	t.Cleanup(func() {
		_ = stats.Close() // not bothered about close errors
	})

	done := func() []fakestatsd.Metric {
		var metrics []fakestatsd.Metric
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			metrics = s.Metrics()
			if len(metrics) == 0 {
				return poll.Continue("no metrics found yet")
			}
			return poll.Success()
		})
		return metrics
	}

	return o11y.WithProvider(ctx, honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: stats,
	})), done
}
