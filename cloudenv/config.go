package cloudenv

import "github.com/circleci/ex/config/secret"

type Config struct {
	Provider      Provider
	RunnerAPIBase string
	CreationToken secret.String

	// Optional
	TestMetadataBaseURL string
	TestSymlinkDir      string
	TestSkipWarmUp      bool
}

func (cfg Config) testMetadataBaseURL(s string) string {
	if cfg.TestMetadataBaseURL != "" {
		return cfg.TestMetadataBaseURL
	}
	return s
}
