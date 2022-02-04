package releases

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	req := httpclient.NewRequest("GET", "/release.txt", time.Minute)
	version := ""
	req.Decoder = d.decodeVersion(&version)
	err := d.client.Call(ctx, req)
	return version, err
}

func (d *Releases) decodeVersion(out *string) func(reader io.Reader) error {
	return func(r io.Reader) error {
		scanner := bufio.NewScanner(r)
		if !scanner.Scan() {
			return ErrNotFound
		}
		txt := scanner.Text()
		*out = txt
		return nil
	}
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
	req := httpclient.NewRequest("GET", "/"+rq.Version+"/checksums.txt", time.Minute)
	r := make([]string, 0)
	req.Decoder = d.decodeDownload(rq, &r)
	err := d.client.Call(ctx, req)
	return r, err
}

func (d *Releases) decodeDownload(rq Requirements, result *[]string) func(reader io.Reader) error {
	return func(reader io.Reader) error {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			txt := scanner.Text()
			if strings.Contains(txt, rq.OS) && strings.Contains(txt, rq.Arch) {
				parts := strings.Split(txt, " ")

				// with some releases the file part is stored with a leading *./
				filename := path.Clean(parts[1][1:])
				filename = strings.TrimPrefix(filename, "/")

				*result = append(*result, fmt.Sprintf("%s/%s/%s", d.baseURL, rq.Version, filename))
			}
		}
		if len(*result) == 0 {
			return ErrNotFound
		}
		return nil
	}
}
