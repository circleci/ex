package redis

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/redisfixture"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMetrics(t *testing.T) {
	ctx := testcontext.Background()
	fix := redisfixture.Setup(ctx, t, redisfixture.Connection{Addr: "localhost:6379"})

	m := NewMetrics("redis", fix.Client)
	gauges := m.Gauges(ctx)

	assert.Check(t, hasKey(gauges, "hits"))
	assert.Check(t, hasKey(gauges, "misses"))
	assert.Check(t, hasKey(gauges, "timeouts"))

	assert.Check(t, hasKey(gauges, "total_connections"))
	assert.Check(t, hasKey(gauges, "idle_connections"))
	assert.Check(t, hasKey(gauges, "stale_connections"))
}

func hasKey(m map[string]float64, key string) bool {
	_, ok := m[key]
	return ok
}
