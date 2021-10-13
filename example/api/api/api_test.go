package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/example/books"
	"github.com/circleci/ex/example/migrations"
)

type fixture struct {
	url   string
	Store *books.Store
}

func startAPI(ctx context.Context, t testing.TB) *fixture {
	t.Helper()

	dbfix := migrations.SetupDB(ctx, t)
	store := books.NewStore(dbfix.TX)
	api := New(ctx, Options{
		Store: store,
	})
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	return &fixture{
		url:   srv.URL,
		Store: store,
	}
}

func (f *fixture) Get(t testing.TB, rawurl string, v interface{}) (statusCode int) {
	t.Helper()

	resp, err := http.Get(f.url + rawurl)
	assert.Assert(t, err)

	defer func() {
		assert.Check(t, resp.Body.Close())
	}()

	if v != nil {
		err = json.NewDecoder(resp.Body).Decode(v)
		assert.Assert(t, err)
	}

	return resp.StatusCode
}

func (f *fixture) Post(t testing.TB, path string, in, out interface{}) (statusCode int) {
	t.Helper()

	var err error
	var body []byte
	if in != nil {
		body, err = json.Marshal(in)
		assert.Assert(t, err)
	}

	resp, err := http.Post(f.url+path, "application/json; charset=utf-8", bytes.NewReader(body))
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
