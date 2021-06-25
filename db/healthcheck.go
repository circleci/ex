package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type HealthCheck struct {
	Name string
	DB   *sqlx.DB
}

func (h *HealthCheck) HealthChecks() (name string, ready, live func(ctx context.Context) error) {
	return h.Name, newPGHealthCheck(h.DB), nil
}

func (h *HealthCheck) MetricName() string {
	return h.Name
}

func (h *HealthCheck) Gauges(_ context.Context) map[string]float64 {
	stats := h.DB.Stats()
	return map[string]float64{
		"in_use":               float64(stats.InUse),
		"idle":                 float64(stats.Idle),
		"wait_count":           float64(stats.WaitCount),
		"wait_duration":        float64(stats.WaitDuration / time.Millisecond),
		"max_idle_closed":      float64(stats.MaxIdleClosed),
		"max_idle_time_closed": float64(stats.MaxIdleTimeClosed),
		"max_lifetime_closed":  float64(stats.MaxLifetimeClosed),
	}
}

// New creates new PostgreSQL health check that verifies the following:
// - doing the ping command
// - selecting postgres version
func newPGHealthCheck(db *sqlx.DB) func(ctx context.Context) error {
	return func(ctx context.Context) (checkErr error) {
		err := db.PingContext(ctx)
		if err != nil {
			checkErr = fmt.Errorf("postgreSQL health check failed on ping: %w", err)
			return
		}

		rows, err := db.QueryContext(ctx, `SELECT VERSION()`)
		if err != nil {
			checkErr = fmt.Errorf("postgreSQL health check failed on select: %w", err)
			return
		}
		defer func() {
			// override checkErr only if there were no other errors
			if err = rows.Close(); err != nil && checkErr == nil {
				checkErr = fmt.Errorf("postgreSQL health check failed on rows closing: %w", err)
			}
		}()

		return
	}
}
