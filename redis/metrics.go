package redis

import (
	"context"

	"github.com/go-redis/redis/v8"
)

type Metrics struct {
	name   string
	client *redis.Client
}

func NewMetrics(name string, client *redis.Client) *Metrics {
	return &Metrics{
		name:   name,
		client: client,
	}
}

func (r *Metrics) MetricName() string {
	return r.name
}

func (r *Metrics) Gauges(_ context.Context) map[string]float64 {
	stats := r.client.PoolStats()
	return map[string]float64{
		"hits":     float64(stats.Hits),     // number of times free connection was found in the pool
		"misses":   float64(stats.Misses),   // number of times free connection was NOT found in the pool
		"timeouts": float64(stats.Timeouts), // number of times a wait timeout occurred

		"total_connections": float64(stats.TotalConns), // number of total connections in the pool
		"idle_connections":  float64(stats.IdleConns),  // number of idle connections in the pool
		"stale_connections": float64(stats.StaleConns), // number of stale connections removed from the pool
	}
}
