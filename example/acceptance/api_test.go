package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/dbfixture"
	"github.com/circleci/ex/testing/runner"
	"github.com/circleci/ex/testing/testcontext"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/example/migrations"
)

func TestE2E(t *testing.T) {
	ctx := testcontext.Background()

	dbfix := migrations.SetupDB(ctx, t)

	var fix *serviceFixture
	assert.Assert(t, t.Run("Start services", func(t *testing.T) {
		fix = runServices(ctx, t, dbfix)
	}))
	t.Cleanup(func() {
		t.Run("Stop services", func(t *testing.T) {
			fix.Stop(t)
		})
	})

	t.Run("Very basic tests", func(t *testing.T) {
		type response struct {
			ID uuid.UUID `json:"id"`
		}

		var res response

		assert.Assert(t, t.Run("Add book to store", func(t *testing.T) {
			status := fix.Post(t, "/api/books", map[string]interface{}{
				"name":  "Wizard of Oz",
				"price": "$6.99",
			}, &res)
			assert.Check(t, cmp.Equal(status, http.StatusOK))
			assert.Check(t, res.ID != uuid.Nil)
		}))

		t.Run("Check book was added", func(t *testing.T) {
			m := make(map[string]interface{})
			status := fix.Get(t, fmt.Sprintf("/api/books/%s", res.ID), &m)
			assert.Check(t, cmp.Equal(status, http.StatusOK))
			assert.Check(t, cmp.DeepEqual(map[string]interface{}{
				"id":    res.ID.String(),
				"name":  "Wizard of Oz",
				"price": "$6.99",
			}, m))
		})
	})

}

func runServices(ctx context.Context, t *testing.T, dbfix *dbfixture.Fixture) *serviceFixture {
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

		"DB_HOST=localhost",
		"DB_PORT=5432",
		"DB_USER=user",
		"DB_PASSWORD=password",
		"DB_SSL=false",
		"DB_NAME="+dbfix.DBName,
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

func (f *serviceFixture) Post(t testing.TB, path string, in, out interface{}) (statusCode int) {
	t.Helper()

	var err error
	var body []byte
	if in != nil {
		body, err = json.Marshal(in)
		assert.Assert(t, err)
	}

	resp, err := http.Post(f.apiBaseURL+path, "application/json; charset=utf-8", bytes.NewReader(body))
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
