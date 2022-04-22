package redis

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"
)

type HealthCheck struct {
	name   string
	client *redis.Client
}

func NewHealthCheck(client *redis.Client, name string) *HealthCheck {
	return &HealthCheck{name: name, client: client}
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
	name = r.name
	if name == "" {
		name = "redis"
	}
	return name, ready, nil
}
