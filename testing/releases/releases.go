package releases

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/circleci/ex/httpclient"
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
	client  *httpclient.Client
}

func New(baseURL string) *Releases {
	return &Releases{baseURL: baseURL, client: httpclient.New(httpclient.Config{
		Name:    "releases",
		BaseURL: baseURL,
	})}
}

// Version gets the latest released version of an artifact.
func (d *Releases) Version(ctx context.Context) (string, error) {
	version := ""

	err := httpclient.NewRequest("GET", "/release.txt", time.Minute).
		AddSuccessDecoder(httpclient.NewStringDecoder(&version)).
		Call(ctx, d.client)

	version = strings.TrimSpace(version)
	if version == "" {
		return version, ErrNotFound
	}
	return version, err
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
	urls := ""

	err := httpclient.NewRequest("GET", "/"+rq.Version+"/checksums.txt", time.Minute).
		AddSuccessDecoder(httpclient.NewStringDecoder(&urls)).
		Call(ctx, d.client)

	if err != nil {
		return nil, err
	}
	return d.decodeDownload(rq, urls)
}

func (d *Releases) decodeDownload(rq Requirements, urls string) ([]string, error) {
	result := make([]string, 0)
	for _, txt := range strings.Split(urls, "\n") {
		if strings.Contains(txt, rq.OS) && strings.Contains(txt, rq.Arch) {
			parts := strings.Split(txt, " ")

			// with some releases the file part is stored with a leading *./
			filename := path.Clean(parts[1][1:])
			filename = strings.TrimPrefix(filename, "/")

			result = append(result, fmt.Sprintf("%s/%s/%s", d.baseURL, rq.Version, filename))
		}
	}
	if len(result) == 0 {
		return result, ErrNotFound
	}
	return result, nil
}
