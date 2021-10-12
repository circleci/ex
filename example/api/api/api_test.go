package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/example/migrations"
	"github.com/circleci/ex/example/service"
)

type fixture struct {
	url string
}

func startAPI(ctx context.Context, t testing.TB) *fixture {
	t.Helper()

	dbfix := migrations.SetupDB(ctx, t)
	api := New(ctx, Options{
		Store: service.NewStore(dbfix.TX),
	})
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	return &fixture{
		url: srv.URL,
	}
}

func get(t testing.TB, rawurl string, v interface{}) (statusCode int) {
	t.Helper()

	resp, err := http.Get(rawurl)
	assert.Assert(t, err)

	defer func() {
		assert.Check(t, resp.Body.Close())
	}()

	err = json.NewDecoder(resp.Body).Decode(v)
	assert.Assert(t, err)

	return resp.StatusCode
}
