package httpnetrecorder_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/httprecorder/httpnetrecorder"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMiddleware(t *testing.T) {
	ctx := testcontext.Background()
	rec := httprecorder.New()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "a string body")
	})
	srv := httptest.NewServer(httpnetrecorder.Middleware(ctx, rec, h))
	t.Cleanup(srv.Close)

	t.Run("Make a request", func(t *testing.T) {
		res, err := http.Get(srv.URL + "/hello")
		assert.Assert(t, err)
		t.Cleanup(func() {
			assert.Check(t, res.Body.Close())
		})
		b, err := io.ReadAll(res.Body)
		assert.Check(t, err)
		assert.Check(t, cmp.Equal("a string body", string(b)))
	})

	t.Run("Check request was present", func(t *testing.T) {
		assert.Check(t, cmp.DeepEqual(
			[]httprecorder.Request{
				{
					Method: "GET",
					URL:    url.URL{Path: "/hello"},
					Header: http.Header{
						"Accept-Encoding": {"gzip"},
						"User-Agent":      {"Go-http-client/1.1"},
					},
					Body: []uint8{},
				},
			},
			rec.AllRequests(),
		))
	})
}
