package release

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/circleci/ex/httpclient"
)

type s3List struct {
	baseURL string
	client  *httpclient.Client
}

func newS3List(baseURL string) *s3List {
	return &s3List{
		baseURL: baseURL,
		client: httpclient.New(
			httpclient.Config{
				Name:    "s3-release-list",
				BaseURL: baseURL,
				Timeout: time.Second * 10,
			}),
	}
}

func (d *s3List) Version(ctx context.Context, releaseType string) (string, error) {
	version := ""
	req := httpclient.NewRequest("GET", "/%s.txt",
		httpclient.RouteParams(releaseType),
		httpclient.Timeout(time.Second),
		httpclient.SuccessDecoder(func(r io.Reader) error {
			var err error
			version, err = d.decodeVersion(r)
			return err
		}),
	)
	return version, d.client.Call(ctx, req)
}

func (d *s3List) Lookup(ctx context.Context, rq Requirements) (*Release, error) {
	var release *Release
	req := httpclient.NewRequest("GET", "/%s/checksums.txt",
		httpclient.RouteParams(rq.Version),
		httpclient.Timeout(time.Second),
		httpclient.SuccessDecoder(func(r io.Reader) error {
			var err error
			release, err = d.decodeRelease(r, rq)
			return err
		}),
	)

	err := d.client.Call(ctx, req)
	if httpclient.HasStatusCode(err, http.StatusNotFound, http.StatusForbidden) {
		return nil, ErrNotFound
	}
	return release, err
}

func (d *s3List) decodeVersion(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return "", ErrNotFound
	}

	return scanner.Text(), nil
}

type Release struct {
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	Version  string `json:"version"`
}

func (d *s3List) decodeRelease(r io.Reader, rq Requirements) (*Release, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		txt := scanner.Text()
		if strings.Contains(txt, rq.Platform) && strings.Contains(txt, rq.Arch) {
			parts := strings.Split(txt, " ")
			filename := parts[1][1:]
			return &Release{
				URL:      fmt.Sprintf("%s/%s/%s", d.baseURL, rq.Version, filename),
				Checksum: parts[0],
				Version:  rq.Version,
			}, nil
		}
	}
	return nil, ErrNotFound
}
