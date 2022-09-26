/*
Package release works with the metadata of execution releases.

It answers questions about current release version and which binary to download for
which os and architecture.
*/
package release

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	lru "github.com/hashicorp/golang-lru"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/worker"
)

var (
	ErrNotFound            = o11y.NewWarning("not found")
	ErrListVersionNotReady = errors.New("list version not ready")
)

type Requirements struct {
	Version  string `json:"version" form:"version"`
	Platform string `json:"os" form:"os"`
	Arch     string `json:"arch" form:"arch"`
}

var downloadVersionRegexp = regexp.MustCompile(`^\d+\.\d+\.\d+-(canary-|dev-)?[0-9a-f]+$`)

func (d *Requirements) Validate() error {
	if d.Version != "" && !downloadVersionRegexp.MatchString(d.Version) {
		return errors.New("version is invalid")
	}
	if d.Platform == "" {
		return errors.New("platform is required")
	}
	if d.Arch == "" {
		return errors.New("arch is required")
	}
	return nil
}

const (
	cacheItemCount     = 1024
	DefaultReleaseType = "release"
)

type List struct {
	name   string
	s3List *s3List

	ready atomicBool

	mu            sync.RWMutex
	cachedVersion map[string]string
	cache         *lru.Cache
	pinnedVersion string
}

func NewList(ctx context.Context, name, pinnedVersion, listBaseURL string,
	additionalReleaseTypes ...string) (*List, error) {
	cache, err := lru.New(cacheItemCount)
	// Failing to init the cache is effectively a type error, not a runtime error
	if err != nil {
		panic(err)
	}

	c := &List{
		name:          name,
		s3List:        newS3List(listBaseURL),
		cache:         cache,
		cachedVersion: map[string]string{DefaultReleaseType: ""},
		pinnedVersion: pinnedVersion,
	}

	for _, releaseType := range additionalReleaseTypes {
		c.cachedVersion[releaseType] = ""
	}

	err = c.storeVersions(ctx)
	if err != nil {
		return c, fmt.Errorf("failed to initialise list: %w", ErrListVersionNotReady)
	}

	return c, nil
}

func (c *List) HealthChecks() (_ string, ready, live func(ctx context.Context) error) {
	return "version-list-" + c.name,
		func(ctx context.Context) error {
			if !c.ready.Get() {
				return ErrListVersionNotReady
			}
			return nil
		}, nil
}

func (c *List) Lookup(ctx context.Context, req Requirements) (resp *Release, err error) {
	ctx, span := o11y.StartSpan(ctx, "release-list: lookup-requirement")
	defer o11y.End(span, &err)
	span.AddField("release.version", req.Version)
	span.AddField("release.platform", req.Platform)
	span.AddField("release.arch", req.Arch)

	if raw, ok := c.cache.Get(req); ok {
		span.AddField("cache_hit", 1)
		resp = raw.(*Release)
		return resp, err
	}
	span.AddField("cache_hit", 0)

	resp, err = c.s3List.Lookup(ctx, req)
	if err == nil {
		c.cache.Add(req, resp)
	}
	return resp, err
}

// LatestFor returns the cached version for a given release type.
func (c *List) LatestFor(releaseType string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.pinnedVersion != "" {
		return c.pinnedVersion
	}
	return c.cachedVersion[releaseType]
}

// Latest returns the cached version for the default release type.
func (c *List) Latest() string {
	return c.LatestFor(DefaultReleaseType)
}

func (c *List) Run(ctx context.Context) error {
	cfg := worker.Config{
		Name:          "release-list",
		MaxWorkTime:   initialStoreTimeout,
		NoWorkBackOff: backoff.NewConstantBackOff(time.Minute),
		WorkFunc: func(ctx context.Context) error {
			err := c.storeVersions(ctx)
			if err != nil {
				return err
			}

			return worker.ErrShouldBackoff
		},
	}

	worker.Run(ctx, cfg)
	return nil
}

// initialStoreTimeout is a var here purely to speed up testing
var initialStoreTimeout = time.Second * 10

func (c *List) storeVersions(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "release-list: store-versions")
	defer o11y.End(span, &err)
	for releaseType := range c.cachedVersion {

		fieldPrefix := fmt.Sprintf("release.%s.", releaseType)

		span.AddField(fieldPrefix+"changed", false)

		version, err := c.s3List.Version(ctx, releaseType)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				err = nil
			}
			return err
		}
		span.AddField(fieldPrefix+"version", version)

		// Avoid write contention by only writing if the version has changed
		// This isn't racy, as there is only one writer (otherwise you'd want a
		// lock over all statements below).
		latestVersion := c.LatestFor(releaseType)
		if version != latestVersion {
			span.AddField(fieldPrefix+"stored", true)
			func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				c.cachedVersion[releaseType] = version
			}()
		}
	}

	c.ready.Set(true)
	return nil
}
