package releases

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

var ErrNotFound = errors.New("not found")

type Requirements struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Releases helps find the latest release and download URL for artifacts using the execution release structure.
type Releases struct {
	baseURL string
}

func New(baseURL string) *Releases {
	return &Releases{baseURL: baseURL}
}

// Version gets the latest released version of an artifact.
func (d *Releases) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", d.baseURL+"/release.txt", nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return "", ErrNotFound
	}
	return d.decodeVersion(res.Body)
}

func (d *Releases) decodeVersion(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return "", ErrNotFound
	}

	return scanner.Text(), nil
}

// ResolveURL gets the raw download URL for a release, based on the requirements (version, OS, arch)
func (d *Releases) ResolveURL(ctx context.Context, rq Requirements) (string, error) {
	r, err := d.resolveURLs(ctx, rq)
	if err != nil {
		return "", err
	}
	return r[0], nil
}

// ResolveURLs gets the raw download URLs for all binaries of a release, based on the requirements (version, OS, arch)
func (d *Releases) ResolveURLs(ctx context.Context, rq Requirements) (map[string]string, error) {
	r, err := d.resolveURLs(ctx, rq)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, p := range r {
		_, file := path.Split(p)
		file = strings.TrimSuffix(file, ".exe")
		result[file] = p
	}
	return result, nil
}

func (d *Releases) resolveURLs(ctx context.Context, rq Requirements) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", d.baseURL+"/"+rq.Version+"/checksums.txt", nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, ErrNotFound
	}
	r, err := d.decodeDownload(res.Body, rq)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (d *Releases) decodeDownload(r io.Reader, rq Requirements) (result []string, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		txt := scanner.Text()
		if strings.Contains(txt, rq.OS) && strings.Contains(txt, rq.Arch) {
			parts := strings.Split(txt, " ")

			// with some releases the file part is stored with a leading *./
			filename := path.Clean(parts[1][1:])
			filename = strings.TrimPrefix(filename, "/")

			result = append(result, fmt.Sprintf("%s/%s/%s", d.baseURL, rq.Version, filename))
		}
	}
	if len(result) == 0 {
		return nil, ErrNotFound
	}
	return result, nil
}
