/*
Package download helps download releases of binaries.
*/
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Downloader struct {
	dir    string
	client http.Client
}

func NewDownloader(timeout time.Duration, dir string) (*Downloader, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("could not absolutify downloader dir: %w", err)
	}

	err = os.MkdirAll(dir, 0755) // #nosec - the downloads are intentionally world-readable
	if err != nil {
		return nil, fmt.Errorf("could not create e2e-test dir: %w", err)
	}

	return &Downloader{
		dir: dir,
		client: http.Client{
			Timeout: timeout,
		},
	}, nil
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

	if isCached(target) {
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

func isCached(target string) bool {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func (d *Downloader) downloadFile(ctx context.Context, url, target string, perm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(target), 0755) // #nosec - the downloads are intentionally world-readable
	if err != nil {
		return fmt.Errorf("could not create directory: %w", err)
	}

	// #nosec - this is an executable binary, these permissions are needed.
	out, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	defer func() {
		err = out.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("cannot create HTTP request: %w", err)
	}

	res, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("could not get URL %q: %w", url, err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %s", res.Status)
	}

	_, err = io.Copy(out, res.Body)
	if err != nil {
		return fmt.Errorf("could not write file %q: %w", target, err)
	}
	return nil
}
