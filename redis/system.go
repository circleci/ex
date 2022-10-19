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

	name := o.Name
	if name == "" {
		name = "redis"
	}
	sys.AddHealthCheck(NewHealthCheck(client, name))
	sys.AddMetrics(NewMetrics(name, client))

	return client
}

// LoadCluster will create a new Redis cluster client, and wire it into the provided System with
// default lifecycle management and observability.
func LoadCluster(o ClusterOptions, sys *system.System) *redis.ClusterClient {
	client := NewCluster(o)

	sys.AddCleanup(func(_ context.Context) error {
		return client.Close()
	})

	name := o.Name
	if name == "" {
		name = "redis"
	}
	sys.AddHealthCheck(NewHealthCheck(client, name))
	sys.AddMetrics(NewMetrics(name, client))

	return client
}
