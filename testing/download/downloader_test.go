package download

import (
	"compress/gzip"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/httprecorder"
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

	ctx := context.Background()

	dir, err := ioutil.TempDir("", "e2e-test")
	assert.NilError(t, err)

	d, err := NewDownloader(10*time.Second, dir)
	assert.NilError(t, err)
	defer func() {
		assert.NilError(t, os.RemoveAll(dir))
	}()

	t.Run("First cold download", func(t *testing.T) {
		target, err := d.Download(ctx, server.URL+"/test/file-1.txt", 0644)

		assert.NilError(t, err)
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
		}})
	})

	t.Run("Second cold download", func(t *testing.T) {
		target, err := d.Download(ctx, server.URL+"/test/file-2.txt", 0644)

		assert.NilError(t, err)
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
		}})
	})

	t.Run("Cached download", func(t *testing.T) {
		originalRequests := recorder.AllRequests()

		target, err := d.Download(ctx, server.URL+"/test/file-2.txt", 0644)

		assert.NilError(t, err)
		assert.Check(t, strings.HasSuffix(target, filepath.Join("test", "file-2.txt")))
		assertFileContents(t, target, "Second compressed file")

		assert.DeepEqual(t, recorder.AllRequests(), originalRequests)
	})

	t.Run("Not found", func(t *testing.T) {
		target, err := d.Download(ctx, server.URL+"/test/file-3.txt", 0644)
		assert.ErrorContains(t, err, "unexpected status")
		assert.Equal(t, target, "")

		requests := recorder.FindRequests("GET", url.URL{Path: "/test/file-3.txt"})
		assert.DeepEqual(t, requests, []httprecorder.Request{{
			Method: "GET",
			URL:    url.URL{Path: "/test/file-3.txt"},
			Header: http.Header{
				"Accept-Encoding": {"gzip"},
				"User-Agent":      {"Go-http-client/1.1"},
			},
			Body: []byte(""),
		}})
	})
}

func assertFileContents(t *testing.T, path, contents string) {
	t.Helper()

	// #nosec G304: Potential file inclusion via variable
	// we construct the vars and ignoring close errors in tests is acceptable.
	f, err := os.Open(path)
	assert.NilError(t, err)
	t.Cleanup(func() {
		assert.Check(t, f.Close())
	})

	b, err := ioutil.ReadAll(f)
	assert.NilError(t, err)

	assert.Equal(t, string(b), contents)
}
