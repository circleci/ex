package dnscache

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/closer"
)

func TestDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello world!")
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	assert.Assert(t, err)

	var lookupCount int64

	resolver := New(Config{
		lookupFunc: func(ctx context.Context, r *net.Resolver, host string) ([]net.IP, error) {
			t.Logf("Got lookup for %q", host)
			atomic.AddInt64(&lookupCount, 1)
			if host == "example.com" {
				return []net.IP{net.ParseIP("127.0.0.1")}, nil
			}
			return nil, fmt.Errorf("unexpected request: %q", host)
		},
	})

	t.Run("Make HTTP requests", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			t.Run(fmt.Sprintf("HTTP request %d", i+1), func(t *testing.T) {
				// Make a new HTTP client each time, to avoid connection pooling
				c := &http.Client{
					Transport: &http.Transport{
						DialContext: DialContext(resolver, nil),
					},
				}

				//nolint:bodyclose // handled by closer
				resp, err := c.Get("http://example.com:" + u.Port())
				assert.Assert(t, err)
				defer closer.ErrorHandler(resp.Body, &err)

				b, err := io.ReadAll(resp.Body)
				assert.Assert(t, err)

				assert.Check(t, cmp.Equal(string(b), "hello world!"))
			})
		}
	})

	t.Run("Check we only got a single lookup", func(t *testing.T) {
		assert.Check(t, cmp.Equal(atomic.LoadInt64(&lookupCount), int64(1)))
	})
}
