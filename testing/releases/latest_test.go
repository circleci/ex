package releases

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestDownloadLatest(t *testing.T) {
	ctx := testcontext.Background()

	const which = "/my-app"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		}
		t.Log(r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))

	dir, err := ioutil.TempDir("", "e2e-test")
	assert.NilError(t, err)

	t.Run("internal binary", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Dir:     dir,
		})
		assert.NilError(t, err)

		b, err := ioutil.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.NilError(t, err)
		assert.Equal(t, string(b), "I am the internal thing to download")
	})

	t.Run("bad pinned", func(t *testing.T) {
		_, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Pinned:  "not-a-ver",
			Dir:     dir,
		})
		assert.ErrorContains(t, err, "resolve failed")
	})

	t.Run("good pinned", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "internal",
			Pinned:  "p.1.n-abc",
			Dir:     dir,
		})
		assert.NilError(t, err)

		b, err := ioutil.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.NilError(t, err)
		assert.Equal(t, string(b), "I am the pinned thing to download")
	})

	t.Run("good pinned", func(t *testing.T) {
		path, err := DownloadLatest(ctx, DownloadConfig{
			BaseURL: srv.URL,
			Which:   "my-app",
			Binary:  "public",
			Dir:     dir,
		})
		assert.NilError(t, err)

		b, err := ioutil.ReadFile(path) //nolint:gosec // it's a test file we just created
		assert.NilError(t, err)
		assert.Equal(t, string(b), "I am the public thing to download")
	})
}
