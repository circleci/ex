package httpserver

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestNew(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	defer cancel()

	r := http.NewServeMux()
	r.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello world!")
	})

	srv, err := New(ctx, Config{
		Name:    "test server",
		Addr:    "localhost:0",
		Handler: r,
	})
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	t.Cleanup(func() {
		assert.Check(t, g.Wait())
	})
	g.Go(func() error {
		return srv.Serve(ctx)
	})

	body, status := get(t, http.DefaultClient, srv.Addr(), "test")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Equal(body, "hello world!"))
}

func TestNew_unix(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	defer cancel()

	r := http.NewServeMux()
	r.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello world!")
	})

	socket := filepath.Join(os.TempDir(), "httpserver-test.sock")

	srv, err := New(ctx, Config{
		Name:    "test server",
		Addr:    socket,
		Handler: r,
		Network: "unix",
	})
	assert.Assert(t, err)

	g, ctx := errgroup.WithContext(ctx)
	t.Cleanup(func() {
		assert.Check(t, g.Wait())
	})
	g.Go(func() error {
		return srv.Serve(ctx)
	})

	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socket)
			},
		},
	}

	body, status := get(t, c, "localhost", "test")
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.Equal(body, "hello world!"))
}

func get(t *testing.T, c *http.Client, baseurl, path string) (string, int) {
	t.Helper()

	r, err := c.Get(fmt.Sprintf("http://%s/%s", baseurl, path))
	assert.Assert(t, err)

	defer func() {
		assert.Assert(t, r.Body.Close())
	}()

	b, err := ioutil.ReadAll(r.Body)
	assert.Assert(t, err)

	return string(b), r.StatusCode
}
