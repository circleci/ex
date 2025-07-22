/*
Package download helps download releases of binaries.
*/
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v5"

	"github.com/circleci/ex/closer"
	"github.com/circleci/ex/httpclient"
	"github.com/circleci/ex/o11y"
)

type Downloader struct {
	dir                    string
	client                 *httpclient.Client
	downloadTimeout        time.Duration
	downloadAttemptTimeout time.Duration
}

type Option func(d *Downloader)

func NewDownloader(timeout time.Duration, dir string, options ...Option) (*Downloader, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("could not absolutify downloader dir: %w", err)
	}

	err = os.MkdirAll(dir, 0755) // #nosec - the downloads are intentionally world-readable
	if err != nil {
		return nil, fmt.Errorf("could not create e2e-test dir: %w", err)
	}

	downloader := &Downloader{
		dir:             dir,
		downloadTimeout: timeout,
		client: httpclient.New(httpclient.Config{
			Name:    "downloader",
			Timeout: timeout,
		}),
	}

	for _, option := range options {
		option(downloader)
	}

	return downloader, nil
}

func AttemptTimeout(timeout time.Duration) Option {
	return func(d *Downloader) {
		d.downloadAttemptTimeout = timeout
	}
}

// Download downloads the file from the rawURL, to a location rooted at the location specified when constructing
// the downloader nested to a file location based on the path part of the rawURL.
func (d *Downloader) Download(ctx context.Context, rawURL string, perm os.FileMode) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("cannot parse URL: %w", err)
	}

	target := d.targetPath(u)
	tmp := target + ".tmp"

	defer func() {
		cleanup := func() {
			defer os.Remove(target) // nolint: errcheck
			defer os.Remove(tmp)    // nolint: errcheck
		}

		// Don't leave half-downloaded or invalid downloads hanging around
		if p := recover(); p != nil {
			cleanup()
			panic(p)
		} else if err != nil {
			cleanup()
		}
	}()

	if isCached(ctx, target) {
		return target, nil
	}

	err = d.downloadFile(ctx, u.String(), tmp, perm)
	if err != nil {
		return "", err
	}

	err = os.Rename(tmp, target)
	if err != nil {
		return "", err
	}

	return target, nil
}

// Remove removes any file that the downloader had previously downloaded from the rawURL
func (d *Downloader) Remove(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("cannot parse URL: %w", err)
	}
	err = os.Remove(d.targetPath(u))
	e := &os.PathError{}
	if errors.As(err, &e) {
		return nil
	}
	return err
}

func (d *Downloader) targetPath(u *url.URL) string {
	return filepath.Join(d.dir, u.Path)
}

func isCached(ctx context.Context, target string) bool {
	info, err := os.Stat(target)
	if err != nil {
		if !os.IsNotExist(err) {
			o11y.AddField(ctx, "downloader_error", err)
		}
		return false
	}
	return !info.IsDir()
}

func (d *Downloader) downloadFile(ctx context.Context, url, target string, perm os.FileMode) (err error) {
	err = os.MkdirAll(filepath.Dir(target), 0755) // #nosec - the downloads are intentionally world-readable
	if err != nil {
		return fmt.Errorf("could not create directory: %w", err)
	}

	//#nosec:G304 // this is an executable binary, these permissions are needed.
	//nolint:bodyclose // handled by closer
	out, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	defer closer.ErrorHandler(out, &err)

	timeout := 30 * time.Second
	if d.downloadAttemptTimeout != 0 {
		timeout = d.downloadAttemptTimeout
	}

	// We've observed instances of the HTTP client hitting a context.DeadlineExceeded error whilst reading the response
	// body in the decoder. Anywhere else in the call this would (likely) lead to the client retrying the request but, in
	// this case, it doesn't because decoder errors are always treated as non-retryable. Therefore we wrap this HTTP call
	// in our own retry mechanism (that only retries for context.DeadlineExceeded errors) to ensure we retry as expected
	// in this case, also ensuring that we clear the file we're downloading to before retrying so that we have a clean
	// slate on the retry. This retry mechanism also respects the timeout set on the HTTP client itself, so that we never
	// retry for longer than requested by the caller.
	download := func() error {
		err := d.client.Call(ctx, httpclient.NewRequest("GET", url,
			httpclient.Timeout(timeout),
			httpclient.SuccessDecoder(func(r io.Reader) error {
				_, err := io.Copy(out, r)
				if err != nil {
					return fmt.Errorf("could not write file %q: %w", target, err)
				}
				return nil
			})))

		if errors.Is(err, context.DeadlineExceeded) {
			if err := d.clearFile(out); err != nil {
				return backoff.Permanent(err)
			}
			return err
		}

		return backoff.Permanent(err)
	}

	_, err = backoff.Retry(ctx, func() (any, error) {
		err := download()
		return nil, err
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxElapsedTime(d.downloadTimeout))
	if err != nil {
		return fmt.Errorf("could not get URL %q: %w", url, err)
	}

	return nil
}

func (d *Downloader) clearFile(file *os.File) error {
	err := file.Truncate(0)
	if err != nil {
		return err
	}

	_, err = file.Seek(0, 0)
	return err
}
