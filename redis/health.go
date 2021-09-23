package redis

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"
)

type HealthCheck struct {
	client *redis.Client
}

func NewHealthCheck(client *redis.Client) *HealthCheck {
	return &HealthCheck{client: client}
}

func (r *HealthCheck) HealthChecks() (name string, ready, live func(ctx context.Context) error) {
	ready = func(ctx context.Context) error {
		pong, err := r.client.Ping(ctx).Result()
		if err != nil {
			return fmt.Errorf("redis ping failed: %w", err)
		}

		if pong != "PONG" {
			return fmt.Errorf("unexpected response for redis ping: %q", pong)
		}

		return nil
	}

	return "redis", ready, nil
}
