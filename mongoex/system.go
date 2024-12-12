package mongoex

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/circleci/ex/system"
)

// Load connects to mongo. The context passed in is expected to carry an o11y provider
// and is only used for reporting (not for cancellation),
func Load(ctx context.Context, appName string, cfg Config, sys *system.System) (*mongo.Client, error) {
	if cfg.Options == nil {
		cfg.Options = options.Client()
	}

	poolMetrics := newPoolMetrics("mongo")
	cfg.Options.SetPoolMonitor(poolMetrics.PoolMonitor(nil))

	client, err := New(ctx, appName, cfg)
	if err != nil {
		return nil, err
	}
	sys.AddCleanup(client.Disconnect)

	sys.AddHealthCheck(&health{
		client: client,
	})
	sys.AddMetrics(poolMetrics)

	return client, nil
}
