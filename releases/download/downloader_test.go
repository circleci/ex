package download

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/httprecorder"
	"github.com/circleci/ex/testing/testcontext"
)

func TestDownloader_Download(t *testing.T) {
	recorder := httprecorder.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := recorder.Record(r)
		if err != nil {
			panic(err)
		}

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/octet-stream")

		zw := gzip.NewWriter(w)
		defer zw.Close()

		switch r.URL.Path {
		case "/test/file-1.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(zw, "First compressed file")
		case "/test/file-2.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(zw, "Second compressed file")
		case "/test/invalid-checksum.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(zw, "Eeeewul!!!")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := testcontext.Background()

	dir, err := os.MkdirTemp("", "e2e-test")
	assert.Assert(t, err)

	d, err := NewDownloader(10*time.Second, dir)
	assert.Assert(t, err)
	defer func() {
		assert.Assert(t, os.RemoveAll(dir))
	}()

	// N.B. The latter sub tests depend on earlier tests, so should be run in order.
	t.Run("First cold download", func(t *testing.T) {
		target, err := d.Download(ctx, server.URL+"/test/file-1.txt", 0644)

		assert.Assert(t, err)
		assert.Check(t, strings.HasSuffix(target, filepath.Join("test", "file-1.txt")))
		assertFileContents(t, target, "First compressed file")

		requests := recorder.FindRequests("GET", url.URL{Path: "/test/file-1.txt"})
		assert.DeepEqual(t, requests, []httprecorder.Request{{
			Method: "GET",
			URL:    url.URL{Path: "/test/file-1.txt"},
			Header: http.Header{
				"Accept-Encoding": {"gzip"},
				"User-Agent":      {"Go-http-client/1.1"},
			},
			Body: []byte(""),
		}}, ignoreO11yCombHeaders)
	})

	url2 := server.URL + "/test/file-2.txt"

	t.Run("Second cold download", func(t *testing.T) {
		target, err := d.Download(ctx, url2, 0644)

		assert.Assert(t, err)
		assert.Check(t, strings.HasSuffix(target, filepath.Join("test", "file-2.txt")))
		assertFileContents(t, target, "Second compressed file")

		requests := recorder.FindRequests("GET", url.URL{Path: "/test/file-2.txt"})
		assert.DeepEqual(t, requests, []httprecorder.Request{{
			Method: "GET",
			URL:    url.URL{Path: "/test/file-2.txt"},
			Header: http.Header{
				"Accept-Encoding": {"gzip"},
				"User-Agent":      {"Go-http-client/1.1"},
			},
			Body: []byte(""),
		}}, ignoreO11yCombHeaders)
	})

	t.Run("Cached download", func(t *testing.T) {
		originalRequests := recorder.AllRequests()

		target, err := d.Download(ctx, url2, 0644)

		assert.Assert(t, err)
		assert.Check(t, strings.HasSuffix(target, filepath.Join("test", "file-2.txt")))
		assertFileContents(t, target, "Second compressed file")

		assert.DeepEqual(t, recorder.AllRequests(), originalRequests, ignoreO11yCombHeaders)
	})

	t.Run("Remove cached and re-download", func(t *testing.T) {
		recorder.Reset()

		err := d.Remove(url2)
		assert.Assert(t, err)

		// It is fine to remove a downloader managed file that is no longer there.
		err = d.Remove(url2)
		assert.Assert(t, err)

		target, err := d.Download(ctx, url2, 0644)
		assert.Assert(t, err)
		assert.Check(t, strings.HasSuffix(target, filepath.Join("test", "file-2.txt")))
		assertFileContents(t, target, "Second compressed file")

		requests := recorder.FindRequests("GET", url.URL{Path: "/test/file-2.txt"})
		assert.DeepEqual(t, requests, []httprecorder.Request{{
			Method: "GET",
			URL:    url.URL{Path: "/test/file-2.txt"},
			Header: http.Header{
				"Accept-Encoding": {"gzip"},
				"User-Agent":      {"Go-http-client/1.1"},
			},
			Body: []byte(""),
		}}, ignoreO11yCombHeaders)
	})

	t.Run("Not found", func(t *testing.T) {
		target, err := d.Download(ctx, server.URL+"/test/file-3.txt", 0644)
		assert.Check(t, cmp.ErrorContains(err, "was 404 (Not Found)"))
		assert.Check(t, cmp.Equal(target, ""))

		requests := recorder.FindRequests("GET", url.URL{Path: "/test/file-3.txt"})
		assert.DeepEqual(t, requests, []httprecorder.Request{{
			Method: "GET",
			URL:    url.URL{Path: "/test/file-3.txt"},
			Header: http.Header{
				"Accept-Encoding": {"gzip"},
				"User-Agent":      {"Go-http-client/1.1"},
			},
			Body: []byte(""),
		}}, ignoreO11yCombHeaders)
	})

	t.Run("remote downloads", func(t *testing.T) {
		urls := []string{
			"https://circleci-binary-releases.s3.amazonaws.com/distributor/1.0.121921-7112fcb8/darwin/amd64/execution.e2e.test",
			"https://circleci-binary-releases.s3.amazonaws.com/output/1.0.17772-56764d3/linux/amd64/receiver",
		}
		for _, remoteURL := range urls {
			target, err := d.Download(ctx, remoteURL, 0644)
			assert.NilError(t, err)

			fi, err := os.Stat(target)
			assert.NilError(t, err)

			assert.Check(t, fi.Size() > 0)
		}
	})

	t.Run("Download except you can't poke target path", func(t *testing.T) {
		fi, err := os.Stat(d.dir + "/test")
		assert.Assert(t, err)

		err = os.Chmod(d.dir+"/test", 0000)
		assert.Assert(t, err)

		defer func() {
			err := os.Chmod(d.dir+"/test", fi.Mode())
			assert.Assert(t, err)
		}()

		_, err = d.Download(ctx, server.URL+"/test/file-1.txt", 0644)

		assert.Check(t, cmp.ErrorContains(err, "permission denied"))
	})
}

func assertFileContents(t *testing.T, path, contents string) {
	t.Helper()

	// #nosec G304: Potential file inclusion via variable
	// we construct the vars and ignoring close errors in tests is acceptable.
	f, err := os.Open(path)
	assert.Assert(t, err)
	t.Cleanup(func() {
		assert.Check(t, f.Close())
	})

	b, err := io.ReadAll(f)
	assert.Assert(t, err)

	assert.Check(t, cmp.Equal(string(b), contents))
}

var ignoreO11yCombHeaders = cmpopts.IgnoreMapEntries(func(key string, values []string) bool {
	return key == "X-Honeycomb-Trace" || key == "Traceparent" || key == "Tracestate"
})
