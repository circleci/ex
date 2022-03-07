package httpserver

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gotest.tools/v3/assert"

	hc "github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/testing/testcontext"
)

func TestHandleClientCancel(t *testing.T) {
	ctx := testcontext.Background()
	r := ginrouter.Default(ctx, "test")

	r.Use(func(c *gin.Context) {
		c.Next()
		if c.Request.URL.Path == "/sleep" {
			assert.Check(t, c.Writer.Status() == 499)
		}
	})
	r.Use(HandleClientCancel)

	r.GET("/", func(c *gin.Context) {
		c.Status(200)
	})
	r.GET("/sleep", func(c *gin.Context) {
		time.Sleep(time.Second)
		c.Status(200)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	client := hc.New(hc.Config{
		Name:    "test",
		BaseURL: server.URL,
		Timeout: 10 * time.Millisecond,
	})

	t.Run("success", func(t *testing.T) {
		req := hc.NewRequest("GET", "/")
		assert.NilError(t, client.Call(ctx, req))
	})

	t.Run("cancel", func(t *testing.T) {
		req := hc.NewRequest("GET", "/sleep", hc.Timeout(time.Millisecond))
		err := client.Call(ctx, req)
		assert.Check(t, errors.Is(err, context.DeadlineExceeded))
	})
}
