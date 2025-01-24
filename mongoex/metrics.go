package mongoex

import (
	"context"
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
)

type poolMetrics struct {
	name string

	mu                 sync.RWMutex
	connClosed         int64
	poolCreated        int64
	connCreated        int64
	getFailed          int64
	getSucceeded       int64
	connReturned       int64
	poolCleared        int64
	poolClosed         int64
	maxPoolSize        uint64
	minPoolSize        uint64
	waitQueueTimeoutMS uint64
}

func newPoolMetrics(name string) *poolMetrics {
	return &poolMetrics{
		name: name,
	}
}

func (c *poolMetrics) MetricName() string {
	return c.name
}

func (c *poolMetrics) Gauges(_ context.Context) map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]float64{
		"connection_closed":     float64(c.connClosed),
		"pool_created":          float64(c.poolCreated),
		"get_failed":            float64(c.getFailed),
		"get_succeeded":         float64(c.getSucceeded),
		"connection_returned":   float64(c.connReturned),
		"pool_cleared":          float64(c.poolCleared),
		"pool_closed":           float64(c.poolClosed),
		"max_pool_size":         float64(c.maxPoolSize),
		"min_pool_size":         float64(c.minPoolSize),
		"wait_queue_timeout_ms": float64(c.waitQueueTimeoutMS),
	}
}

func (c *poolMetrics) PoolMonitor(parent *event.PoolMonitor) *event.PoolMonitor {
	return &event.PoolMonitor{
		Event: func(e *event.PoolEvent) {
			if parent != nil {
				parent.Event(e)
			}
			c.updateStats(e)
		},
	}
}

func (c *poolMetrics) updateStats(e *event.PoolEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch e.Type {
	case event.ConnectionClosed:
		c.connClosed++
	case event.ConnectionPoolCreated:
		c.poolCreated++
	case event.ConnectionCreated:
		c.connCreated++
	case event.ConnectionCheckOutFailed:
		c.getFailed++
	case event.ConnectionCheckedOut:
		c.getSucceeded++
	case event.ConnectionCheckedIn:
		c.connReturned++
	case event.ConnectionPoolCleared:
		c.poolCleared++
	case event.ConnectionPoolClosed:
		c.poolClosed++
	}

	if e.PoolOptions != nil {
		c.maxPoolSize = e.PoolOptions.MaxPoolSize
		c.minPoolSize = e.PoolOptions.MinPoolSize
		c.waitQueueTimeoutMS = e.PoolOptions.WaitQueueTimeoutMS
	}
}
