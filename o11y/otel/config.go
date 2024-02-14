package otel

import o11yconf "github.com/circleci/ex/config/o11y"

type Config struct {
	o11yconf.Config

	OtelDataset     string
	GrpcHostAndPort string
}
