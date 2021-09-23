package redis

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/redisfixture"
	"github.com/circleci/ex/testing/testcontext"
)

func TestHealthCheck(t *testing.T) {
	ctx := testcontext.Background()
	fix := redisfixture.Setup(ctx, t)

	h := NewHealthCheck(fix.Client)
	checks, ready, live := h.HealthChecks()
	assert.Check(t, cmp.Equal(checks, "redis"))
	assert.Check(t, cmp.Nil(live))

	err := ready(ctx)
	assert.Check(t, err)
}
