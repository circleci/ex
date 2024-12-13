package releases

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/testcontext"
)

func TestDownloadLatest(t *testing.T) {
	ctx := testcontext.Background()

	const which = "/my-app"
	slowAttempt := 0
	mu := sync.Mutex{}
	rec := httprecorder.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := rec.Record(r)
		assert.NilError(t, err)

		switch r.URL.Path {
		case which + "/release.txt":
			_, _ = io.WriteString(w, "1.2.3-abc\n")
			return
		case which + "/1.2.3-abc/checksums.txt":
			_, _ = io.WriteString(w, checksum)
			return
		case which + "/p.1.n-abc/checksums.txt":
			_, _ = io.WriteString(w, checksum)
			return
		case which + "/1.2.3-abc/" + runtime.GOOS + "/" + runtime.GOARCH + "/internal":
			_, _ = io.WriteString(w, "I am the internal thing to download")
			return
		case which + "/1.2.3-abc/" + runtime.GOOS + "/" + runtime.GOARCH + "/public":
			_, _ = io.WriteString(w, "I am the public thing to download")
			return
		case which + "/p.1.n-abc/" + runtime.GOOS + "/" + runtime.GOARCH + "/internal":
			_, _ = io.WriteString(w, "I am the pinned thing to download")
			return
		case which + "/slow/checksums.txt":
			_, _ = io.WriteString(w, checksum)
		case which + "/slow/" + runtime.GOOS + "/" + runtime.GOARCH + "/slow":
			mu.Lock()
			defer mu.Unlock()
			if slowAttempt == 0 {
				time.Sleep(3 * time.Second)
				slowAttempt++
			}
			_, _ = io.WriteString(w, "this is the slow file")
		}
		t.Log(r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))

	dir, err := os.MkdirTemp("", "e2e-test")
	assert.Assert(t, err)

	t.Run("internal binary", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Dir:     dir,
		})
		assert.Assert(t, err)

		// Check that we don't double up the which path
		assert.Check(t, !strings.Contains(path, "my-app/my-app"))

		b, err := os.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.Assert(t, err)
		assert.Check(t, cmp.Equal(string(b), "I am the internal thing to download"))
	})

	t.Run("bad pinned", func(t *testing.T) {
		_, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Pinned:  "not-a-ver",
			Dir:     dir,
		})
		assert.Check(t, cmp.ErrorContains(err, "resolve failed"))
	})

	t.Run("good pinned", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Pinned:  "p.1.n-abc",
			Dir:     dir,
		})
		assert.Assert(t, err)

		b, err := os.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.Assert(t, err)
		assert.Check(t, cmp.Equal(string(b), "I am the pinned thing to download"))
	})

	t.Run("good pinned", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "public",
			Dir:     dir,
		})
		assert.Assert(t, err)

		b, err := os.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.Assert(t, err)
		assert.Check(t, cmp.Equal(string(b), "I am the public thing to download"))
	})

	t.Run("slow with short timeout", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL:        srv.URL,
			Which:          "my-app",
			Binary:         "slow",
			Dir:            dir,
			Pinned:         "slow",
			AttemptTimeout: 2 * time.Second,
		})

		assert.Assert(t, err)

		b, err := os.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.Assert(t, err)
		assert.Check(t, cmp.Equal(string(b), "this is the slow file"))

		fileURL, err := url.Parse(which + "/slow/" + runtime.GOOS + "/" + runtime.GOARCH + "/slow")
		assert.Assert(t, err)
		requests := rec.FindRequests("GET", *fileURL)
		assert.Check(t, cmp.Equal(2, len(requests)))
	})
}
