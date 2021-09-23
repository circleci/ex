package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/runner"
	"github.com/circleci/ex/testing/testcontext"
)

func TestE2E(t *testing.T) {
	ctx := testcontext.Background()

	var fix *serviceFixture
	assert.Assert(t, t.Run("Start services", func(t *testing.T) {
		fix = runServices(ctx, t)
	}))
	t.Cleanup(func() {
		t.Run("Stop services", func(t *testing.T) {
			fix.Stop(t)
		})
	})

	t.Run("Some actual tests", func(t *testing.T) {
		m := make(map[string]interface{})
		status := fix.Get(t, "/api/hello", &m)
		assert.Check(t, cmp.Equal(status, http.StatusOK))
		assert.Check(t, cmp.DeepEqual(m, map[string]interface{}{"hello": "world!"}))
	})

}

func runServices(ctx context.Context, t *testing.T) *serviceFixture {
	t.Helper()
	ctx, span := o11y.StartSpan(ctx, "acceptance: run_services")
	defer o11y.End(span, nil)

	var cleanups []func()
	// TODO: Cleanups

	r := runner.New(
		"ADMIN_ADDR=localhost:0",
		"O11Y_STATSD=localhost:8125",
		"O11Y_HONEYCOMB=false",
		"O11Y_FORMAT=color",
		"O11Y_ROLLBAR_ENV=testing",
	)

	var apiResult *runner.Result

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		apiResult, err = r.Run("api", apiTestBinary,
			"SHUTDOWN_DELAY=0",
			"API_ADDR=localhost:0",
		)
		return err
	})
	assert.Assert(t, g.Wait())

	return &serviceFixture{
		runner:     r,
		apiBaseURL: apiResult.APIAddr(),
		cleanups:   cleanups,
	}
}

type serviceFixture struct {
	runner *runner.Runner

	apiBaseURL string

	cleanups []func()
}

func (f *serviceFixture) Stop(t *testing.T) {
	t.Helper()
	if f == nil {
		return
	}

	err := f.runner.Stop()
	assert.Check(t, err)

	for _, cleanup := range f.cleanups {
		cleanup()
	}
}

func (f *serviceFixture) Get(t testing.TB, path string, out interface{}) (statusCode int) {
	t.Helper()

	var err error
	resp, err := http.Get(f.apiBaseURL + path)
	assert.Assert(t, err)

	defer func() {
		assert.Check(t, resp.Body.Close())
	}()

	if resp.StatusCode < 300 && out != nil {
		err = json.NewDecoder(resp.Body).Decode(out)
		assert.Assert(t, err)
	}

	return resp.StatusCode
}
