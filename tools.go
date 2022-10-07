//go:build tools

package ex

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/gwatts/rootcerts/gencerts"
	_ "github.com/rinchsan/gosimports/cmd/gosimports"
	_ "gotest.tools/gotestsum"
)
