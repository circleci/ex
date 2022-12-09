package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/circleci/ex/o11y"
)

type HealthCheck struct {
	name   string
	client redis.UniversalClient
}

func NewHealthCheck(client redis.UniversalClient, name string) *HealthCheck {
	return &HealthCheck{name: name, client: client}
}

func (r *HealthCheck) HealthChecks() (name string, ready, live func(ctx context.Context) error) {
	ready = func(ctx context.Context) (err error) {
		start := time.Now()
		m := o11y.FromContext(ctx).MetricsProvider()

		defer func() {
			t := time.Since(start)
			tags := []string{
				fmt.Sprintf("%s:%t", "error", err != nil),
				fmt.Sprintf("%s:%s", "name", name),
			}
			_ = m.TimeInMilliseconds("redis_healthcheck_duration", float64(t.Milliseconds()), tags, 1.0)
		}()

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
