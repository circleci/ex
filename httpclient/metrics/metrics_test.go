package metrics

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMetrics(t *testing.T) {

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond * 200)
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
			assert.NilError(t, err)
		}()

		concurrentRequests := 100
		maxConnections := 90 // close to max concurrency, but we should see some waiting
		cl := httpclient.New(httpclient.Config{
			Name:                  "test-client",
			BaseURL:               s.URL,
			MaxConnectionsPerHost: maxConnections,
			Tracer:                tracer,
		})

		r := httpclient.NewRequest("GET", "/test/%s", time.Second, "foo")
		err := cl.Call(ctx, r)
		assert.NilError(t, err)

		r = httpclient.NewRequest("GET", "/test/%s", time.Second, "foo")
		err = cl.Call(ctx, r)
		assert.NilError(t, err)

		var wg sync.WaitGroup
		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test", time.Second)
				err := cl.Call(ctx, r)
				assert.NilError(t, err)
			}()
		}
		wg.Wait()

		gauges := tracer.Gauges(ctx)
		g, ok := gauges["in_flight"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 2))
		assert.Check(t, cmp.Equal(g[0].Val, float64(0)))
		assert.Check(t, cmp.Equal(g[1].Val, float64(100)))
		g, ok = gauges["pool_avail_estimate"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 1))

		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test/thing", time.Second)
				err := cl.Call(ctx, r)
				assert.NilError(t, err)
			}()
		}

		wg.Wait()

		cancel()
		metrics, err := done() // wait for the stats server to stop
		assert.NilError(t, err)

		assert.Check(t, cmp.Equal(len(metrics), 974))

		assertIn(t, metrics, "delayed:true", 10, 30)
		assertIn(t, metrics, "starved:true", 10, 30)
		idle := (concurrentRequests - maxConnections) + concurrentRequests
		assertIn(t, metrics, "idle:false", idle, idle)
		assertIn(t, metrics, "reused:false", maxConnections, maxConnections)
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
			assert.NilError(t, err)
		}()

		concurrentRequests := 50
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
				r := httpclient.NewRequest("GET", "/test", time.Second)
				err := cl.Call(ctx, r)
				assert.NilError(t, err)
			}()
		}
		wg.Wait()

		gauges := tracer.Gauges(ctx)
		g, ok := gauges["in_flight"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 2))
		assert.Check(t, cmp.Equal(g[0].Val, float64(0)))
		assert.Check(t, cmp.Equal(g[1].Val, float64(50)))
		g, ok = gauges["pool_avail_estimate"]
		assert.Check(t, ok)
		assert.Check(t, cmp.Equal(len(g), 1))

		wg.Add(concurrentRequests)

		for i := 0; i < concurrentRequests; i++ {
			go func() {
				defer wg.Done()
				r := httpclient.NewRequest("GET", "/test/thing", time.Second)
				err := cl.Call(ctx, r)
				assert.NilError(t, err)
			}()
		}

		wg.Wait()

		cancel()
		metrics, err := done() // wait for the stats server to stop
		assert.NilError(t, err)

		assert.Check(t, len(metrics) > 400, len(metrics))
		assert.Check(t, len(metrics) < 520, len(metrics))

		assertIn(t, metrics, "delayed:true", 0, 0)
		assertIn(t, metrics, "starved:true", 0, 0)
		assertIn(t, metrics, "idle:false", concurrentRequests, concurrentRequests)
		assertIn(t, metrics, "reused:false", concurrentRequests, concurrentRequests)
	})
}

/*

   metrics_test.go:107: assertion failed: 968 (int) != 974 (int)
   metrics_test.go:109: assertion failed: expression is false: count <= max: count:26 > max:20
   metrics_test.go:110: assertion failed: expression is false: count <= max: count:26 > max:20
   metrics_test.go:113: assertion failed: expression is false: count >= min: count:84 < min:90

    metrics_test.go:182: assertion failed: expression is false: count <= max: count:2 > max:0
    metrics_test.go:183: assertion failed: expression is false: count <= max: count:4 > max:0
    metrics_test.go:185: assertion failed: expression is false: count >= min: count:96 < min:100

*/

func assertIn(t *testing.T, ls []string, what string, min, max int) {
	t.Helper()
	count := 0
	for _, l := range ls {
		if strings.Contains(l, what) {
			count++
		}
	}
	assert.Check(t, count >= min, "count:%d < min:%d", count, min)
	assert.Check(t, count <= max, "count:%d > max:%d", count, max)
}

func setupMetrics(t *testing.T, ctx context.Context) (context.Context, func() ([]string, error)) {
	socket := filepath.Join(os.TempDir(), "httpclient-trace-test.sock")
	t.Cleanup(func() {
		_ = os.Remove(socket) // ignore errors
	})

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	assert.NilError(t, err)
	stats, err := statsd.New(conn.LocalAddr().String())
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	t.Cleanup(func() {
		_ = stats.Close() // not bothered about close errors
	})
	var metrics []string
	g.Go(udpServerFn(ctx, conn, &metrics))

	done := func() ([]string, error) {
		e := g.Wait()
		return metrics, e
	}
	return o11y.WithProvider(ctx, honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: stats,
	})), done
}

func udpServerFn(ctx context.Context, r io.ReadCloser, rb *[]string) func() error {
	return func() error {
		done := make(chan struct{})
		go func() {
			scanner := bufio.NewScanner(r)
			scanner.Split(bufio.ScanLines)
			for scanner.Scan() {
				*rb = append(*rb, scanner.Text())
			}
			close(done)
		}()

		for {
			select {
			case <-ctx.Done():
				_ = r.Close()
				<-done
				return nil
			case <-done:
				return nil
			}
		}
	}
}
