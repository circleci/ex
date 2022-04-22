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
	fix := redisfixture.Setup(ctx, t, redisfixture.Connection{Addr: "localhost:6379"})

	h := NewHealthCheck(fix.Client, "redis-cache")
	checks, ready, live := h.HealthChecks()
	assert.Check(t, cmp.Equal(checks, "redis-cache"))
	assert.Check(t, cmp.Nil(live))

	err := ready(ctx)
	assert.Check(t, err)
}
