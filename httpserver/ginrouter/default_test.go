package ginrouter

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/internal/syncbuffer"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
)

func TestMiddleware(t *testing.T) {
	b := &syncbuffer.SyncBuffer{}

	p, err := otel.New(otel.Config{
		Metrics: &statsd.NoOpClient{},
		Writer:  b,
	})
	assert.NilError(t, err)
	ctx := o11y.WithProvider(context.Background(), p)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := Default(ctx, "test server")
	r.GET("/foo", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.GET("/slow", func(c *gin.Context) {
		time.Sleep(500 * time.Millisecond)
		c.Status(http.StatusInternalServerError)
	})

	srv, err := httpserver.New(ctx, httpserver.Config{
		Name:    "test-server",
		Addr:    "localhost:0",
		Handler: r,
	})
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

	t.Run("Check we can get a 200 response", func(t *testing.T) {
		b.Reset()
		err = client.Call(ctx, httpclient.NewRequest("GET", "/foo"))
		assert.Assert(t, err)
		checkO11yHasStatus(t, b, "200")
	})

	t.Run("Check we can get a 499 response", func(t *testing.T) {
		b.Reset()
		ctx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()

		err = client.Call(ctx, httpclient.NewRequest("GET", "/slow"))
		assert.Check(t, cmp.ErrorIs(err, context.DeadlineExceeded))
		checkO11yHasStatus(t, b, "499")
	})
}

func checkO11yHasStatus(t *testing.T, b *syncbuffer.SyncBuffer, needle string) {
	t.Helper()
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		s := b.String()
		scanner := bufio.NewScanner(strings.NewReader(s))
		for scanner.Scan() {
			text := scanner.Text()
			if !strings.Contains(text, "GET /") {
				continue
			}
			if strings.Contains(text, needle) {
				return poll.Success()
			}
		}
		return poll.Continue("%q does not contain %q", s, needle)
	})
	t.Log(b.String())
}
