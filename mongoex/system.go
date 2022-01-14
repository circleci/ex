package mongoex

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"

	"github.com/circleci/ex/system"
)

// Load connects to mongo. The context passed in is expected to carry an o11y provider
// and is only used for reporting (not for cancellation),
func Load(ctx context.Context, dbName, appName string, cfg Config, sys *system.System) (*mongo.Database, error) {
	poolMetrics := newPoolMetrics("mongo")
	cfg.PoolMonitor = poolMetrics.PoolMonitor(nil)

	client, err := New(ctx, appName, cfg)
	if err != nil {
		return nil, err
	}
	sys.AddCleanup(client.Disconnect)

	sys.AddHealthCheck(&health{
		client: client,
	})
	sys.AddMetrics(poolMetrics)

	return client.Database(dbName), nil
}
