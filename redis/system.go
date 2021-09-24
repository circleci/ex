package redis

import (
	"context"

	"github.com/go-redis/redis/v8"

	"github.com/circleci/ex/system"
)

// Load will create a new Redis client, and wire it into the provided System with
// default lifecycle management and observability.
func Load(o Options, sys *system.System) *redis.Client {
	client := New(o)

	sys.AddCleanup(func(_ context.Context) error {
		return client.Close()
	})

	sys.AddHealthCheck(NewHealthCheck(client))
	sys.AddMetrics(NewMetrics("redis", client))

	return client
}
