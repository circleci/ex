package o11ygin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMiddleware(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	defer cancel()

	r := gin.New()
	r.Use(
		Middleware(o11y.FromContext(ctx), "test-server", nil),
		Recovery(),
	)
	r.UseRawPath = true

	r.POST("/api/:id", func(c *gin.Context) {
		switch id := c.Param("id"); id {
		case "exists":
			c.String(http.StatusOK, id)
		case "panic":
			panic("oh noes!")
		default:
			c.Status(http.StatusNotFound)
		}
	})

	srv, err := httpserver.New(ctx, "test-server", "127.0.0.1:0", r)
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return srv.Serve(ctx)
	})
	t.Cleanup(func() {
		assert.Check(t, g.Wait())
	})

	client := httpclient.New(httpclient.Config{
		Name:    "test-client",
		BaseURL: "http://" + srv.Addr(),
	})

	t.Run("Hit an ID that exists", func(t *testing.T) {
		err = client.Call(ctx, httpclient.NewRequest("POST", "/api/%s", time.Second, "exists"))
		assert.Assert(t, err)
	})

	t.Run("Hit an ID that does not exist", func(t *testing.T) {
		err = client.Call(ctx, httpclient.NewRequest("POST", "/api/%s", time.Second, "does-not-exist"))
		assert.Check(t, httpclient.HasStatusCode(err, http.StatusNotFound))
	})

	t.Run("Hit an ID that panics", func(t *testing.T) {
		resp, err := http.Post("http://"+srv.Addr()+"/api/panic", "", nil)
		assert.Assert(t, err)
		_ = resp.Body.Close()
		assert.Check(t, cmp.Equal(resp.StatusCode, http.StatusInternalServerError))
	})
}
