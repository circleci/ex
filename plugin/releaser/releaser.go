package releaser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/circleci/ex/releases/compiler"
	"github.com/circleci/ex/releases/releaser"
)

// Namespace is an enum representing the valid namespaces that a binary can be uploaded to
type Namespace int

const (
	// NamespacePlugin is the namespace used to upload plugin binaries
	NamespacePlugin Namespace = iota
	// NamespaceSubcommand is the namespace used to upload subcommand binaries
	NamespaceSubcommand
)

func (n Namespace) String() string {
	switch n {
	case NamespacePlugin:
		return "task-agent-plugins"
	case NamespaceSubcommand:
		return "task-agent-subcommands"
	default:
		return "task-agent-plugins"
	}
}

// Releaser is responsible for compiling and uploading the Go binaries for task-agent plugins/subcommands
// in a standardised way
type Releaser struct {
	plugin  string
	version string

	buildDir  string
	platforms map[string][]string

	bucket    string
	namespace string
	client    *s3.Client
}

type Config struct {
	// Plugin is the name of the plugin/subcommand binary
	Plugin string
	// Version is the version of the binary to release
	Version string

	// Platforms is a map of OSes to architectures that the target binary will be compiled for
	Platforms map[string][]string

	// Bucket is the name of the S3 bucket that the compiled binaries will be uploaded to
	Bucket string
	// Namespace is the namespace prefix to use in the upload key for the binary. If not provided then
	// defaults to `task-agent-plugins`. Valid options are: `task-agent-plugins` or `task-agent-subcommands`
	Namespace Namespace

	// Client is an S3 client
	Client *s3.Client
}

// New constructs a new Releaser
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
				"arm64",
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

		bucket:    cfg.Bucket,
		namespace: cfg.Namespace.String(),
		client:    cfg.Client,
	}, nil
}

// Opts are the options available when running the Releaser
type Opts struct {
	// Source is the source package of the plugin to compile and upload
	Source string
	// WorkingDir is the directory from which the compilation should be run from
	WorkingDir string
	// LDFlags are any linker flags to be included when building the binary
	LDFlags string
}

// Run compiles the binary at opts.Source for each of the configured platforms and then uploads the
// resulting binaries to the configured S3 bucket using the key with the format:
// `namespace/plugin_name/version/os/arch/plugin_name`
func (r Releaser) Run(ctx context.Context, opts Opts) error {
	if opts.Source == "" {
		return fmt.Errorf("source must not be empty")
	}

	if opts.WorkingDir == "" {
		opts.WorkingDir = "."
	}

	cleanup, err := r.build(ctx, opts.Source, opts.WorkingDir, opts.LDFlags)
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

func (r Releaser) build(ctx context.Context, source, workingDir, extraLDFlags string) (func(), error) {
	ldFlags := []string{
		"-s -w", // remove debug information from released binaries
		extraLDFlags,
	}
	comp := compiler.New(compiler.Config{
		BaseDir: r.buildDir,
		LDFlags: strings.Join(ldFlags, " "),
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
					"GOOS=" + os,
					"GOARCH=" + arch,
				},
			})
		}
	}

	return cleanup, comp.Run(ctx)
}

func (r Releaser) upload(ctx context.Context) error {
	app := filepath.Join(r.namespace, r.plugin)

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
