package testcontext

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

func TestBackground_MetricsProvider(t *testing.T) {
	ctx := Background()
	metrics := o11y.FromContext(ctx).MetricsProvider()

	err := metrics.Gauge("gauge", 1, nil, 1)
	assert.Assert(t, err)
}
