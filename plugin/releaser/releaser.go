package releaser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/circleci/ex/releases/compiler"
	"github.com/circleci/ex/releases/releaser"
)

type Releaser struct {
	plugin  string
	version string

	buildDir  string
	platforms map[string][]string

	bucket string
	client *s3.Client
}

type Config struct {
	Plugin  string
	Version string

	Platforms map[string][]string

	Bucket string
	Client *s3.Client
}

func New(cfg Config) (Releaser, error) {
	if cfg.Plugin == "" || cfg.Version == "" {
		return Releaser{}, fmt.Errorf("plugin and version must be provided")
	}

	if cfg.Client == nil {
		// TODO: Instantiate client if none provided
		return Releaser{}, fmt.Errorf("client must be provided")
	}

	if cfg.Bucket == "" {
		cfg.Bucket = "circleci-binary-releases"
	}

	if len(cfg.Platforms) == 0 {
		cfg.Platforms = map[string][]string{
			"linux": {
				"amd64",
				"arm",
				"arm64",
				"ppc64le",
				"s390x",
			},
			"darwin": {
				"amd64",
				"arm64",
			},
			"windows": {
				"amd64",
			},
		}
	}

	buildDir, err := os.MkdirTemp("", "")
	if err != nil {
		return Releaser{}, err
	}

	return Releaser{
		plugin:  cfg.Plugin,
		version: cfg.Version,

		buildDir:  buildDir,
		platforms: cfg.Platforms,

		bucket: cfg.Bucket,
		client: cfg.Client,
	}, nil
}

type Opts struct {
	Source     string
	WorkingDir string
}

func (r Releaser) Run(ctx context.Context, opts Opts) error {
	if opts.Source == "" {
		return fmt.Errorf("source must not be empty")
	}

	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}

	cleanup, err := r.build(ctx, opts.Source, opts.WorkingDir)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	err = r.upload(ctx)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	return nil
}

func (r Releaser) build(ctx context.Context, source, workingDir string) (func(), error) {
	comp := compiler.New(compiler.Config{
		BaseDir: r.buildDir,
	})

	cleanup := func() {
		comp.Cleanup()
		_ = os.RemoveAll(r.buildDir)
	}

	for os, archs := range r.platforms {
		for _, arch := range archs {
			comp.Add(compiler.Work{
				Name:   filepath.Join(r.plugin, os, arch, r.plugin),
				Source: source,
				Target: workingDir,
				Environment: []string{
					"CGO_ENABLED=0",
					"GOOS=" + os,
					"GOARCH=" + arch,
				},
			})
		}
	}

	return cleanup, comp.Run(ctx)
}

func (r Releaser) upload(ctx context.Context) error {
	app := filepath.Join("task-agent-plugins", r.plugin)

	rel := releaser.NewWithClient(r.client)
	err := rel.Publish(ctx, releaser.PublishParameters{
		App:     app,
		Bucket:  r.bucket,
		Path:    filepath.Join(r.buildDir, r.plugin),
		Version: r.version,
	})
	if err != nil {
		return err
	}

	return rel.Release(ctx, releaser.ReleaseParameters{
		App:     app,
		Bucket:  r.bucket,
		Version: r.version,
	})
}
