package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

func TestAPI_Healthy(t *testing.T) {
	baseurl := startAPI(t, &mockHealthChecks{
		ready: func(_ context.Context) error {
			return nil
		},
		live: func(_ context.Context) error {
			return nil
		},
	})

	body, status := get(t, baseurl, "live")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Contains(body, `"status":"OK"`))

	body, status = get(t, baseurl, "ready")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Contains(body, `"status":"OK"`))
}

func TestAPI_Unavailable(t *testing.T) {
	baseurl := startAPI(t, &mockHealthChecks{
		ready: func(_ context.Context) error {
			return nil
		},
		live: func(_ context.Context) error {
			return errors.New("dead")
		},
	})

	body, status := get(t, baseurl, "live")
	assert.Check(t, cmp.Equal(status, http.StatusServiceUnavailable))
	assert.Check(t, cmp.Contains(body, `"status":"Unavailable"`))

	body, status = get(t, baseurl, "ready")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Contains(body, `"status":"OK"`))
}

func TestAPI_NotReady(t *testing.T) {
	baseurl := startAPI(t, &mockHealthChecks{
		ready: func(_ context.Context) error {
			return errors.New("not ready")
		},
		live: func(_ context.Context) error {
			return nil
		},
	})

	body, status := get(t, baseurl, "live")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Contains(body, `"status":"OK"`))

	body, status = get(t, baseurl, "ready")
	assert.Check(t, cmp.Equal(status, http.StatusServiceUnavailable))
	assert.Check(t, cmp.Contains(body, `"status":"Unavailable"`))
}

func TestAPI_Debug(t *testing.T) {
	baseurl := startAPI(t)

	t.Run("standard", func(t *testing.T) {
		// The index page html
		body, status := get(t, baseurl, "debug/pprof")
		assert.Check(t, cmp.Equal(status, http.StatusOK))
		assert.Check(t, cmp.Contains(body, `Types of profiles available`))

		// Index served sub profiles
		body, status = get(t, baseurl, "debug/pprof/heap")
		assert.Check(t, cmp.Equal(status, http.StatusOK))
		assert.Check(t, len(body) > 100) // we should have some content

		_, status = get(t, baseurl, "debug/pprof/mutex")
		assert.Check(t, cmp.Equal(status, http.StatusOK))

	})

	// The special profiles
	for _, p := range []string{"cmdline", "profile", "symbol", "trace"} {
		t.Run(p, func(t *testing.T) {
			_, status := get(t, baseurl, fmt.Sprintf("debug/pprof/%s?seconds=1", p))
			assert.Check(t, cmp.Equal(status, http.StatusOK))
		})
	}

	t.Run("not-found", func(t *testing.T) {
		_, status := get(t, baseurl, "debug/pprof/nowt")
		assert.Check(t, cmp.Equal(status, http.StatusNotFound))
	})

}

type mockHealthChecks struct {
	ready, live func(ctx context.Context) error
}

func (m *mockHealthChecks) HealthChecks() (name string, ready, live func(ctx context.Context) error) {
	return "mock healthcheck", m.ready, m.live
}

func startAPI(t *testing.T, checked ...system.HealthChecker) string {
	t.Helper()

	ctx := testcontext.Background()

	api, err := New(ctx, checked)
	assert.Assert(t, err)

	srv := httptest.NewServer(api.Handler())
	t.Cleanup(func() {
		srv.Close()
	})

	return srv.URL
}

func get(t *testing.T, baseurl, path string) (string, int) {
	t.Helper()

	r, err := http.Get(fmt.Sprintf("%s/%s", baseurl, path))
	assert.Assert(t, err)

	defer func() {
		assert.Assert(t, r.Body.Close())
	}()

	b, err := io.ReadAll(r.Body)
	assert.Assert(t, err)

	return string(b), r.StatusCode
}
