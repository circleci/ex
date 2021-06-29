package releases

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestReleases_Version(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, "1.2.3-abc")
	}))

	rel := New(srv.URL)
	ver, err := rel.Version(ctx)
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, "1.2.3-abc"))
}

func TestReleases_ResolveURL(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.2.3-abc/checksums.txt" {
			t.Log(r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `0e4915e71e0c59ab90e7986eb141a5d69baa098c9df122e05e34b46fd2144e1b *darwin/amd64/agent
6615fd0de8f60b07d6659f5e84dee29986d5a7dfbd4b0169dcd9f0d0cf057fdd *darwin/arm64/agent
24a3df3bc4b67763e465d20118e5856b60a1cb70147195177f03f3e948c0ae86 *linux/amd64/agent
42199f7de7bbac08653c1c6ddb16df1c9838f1e852f4583d5dcf20b478055532 *linux/arm64/agent
51ff01417a07dab940eb69078997ec607c0cde6e317c7ff1cdbe353217e7f04e *linux/arm/agent
2706af5f6e6dd19c9fe38725383abcb83da68bc729632dabca2d2bb190591162 *windows/amd64/agent.exe
51ff01417a07dab940eb69078997ec607c0cde6e317c7ff1cdbe353217e7f04g *./linux/arm1/agent
51ff01417a07dab940eb69078997ec607c0cde6e317c7ff1cdbe353217e7f04h */linux/arm2/agent`)
	}))

	rel := New(srv.URL)
	ver, err := rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "amd64",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/amd64/agent"))
	ver, err = rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "arm1",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/arm1/agent"))
	ver, err = rel.ResolveURL(ctx, Requirements{
		Version: "1.2.3-abc",
		OS:      "linux",
		Arch:    "arm2",
	})
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(ver, srv.URL+"/1.2.3-abc/linux/arm2/agent"))
}
