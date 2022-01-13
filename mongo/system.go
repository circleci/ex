package mongo

import (
	"context"
	"crypto/tls"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/circleci/ex/rootcerts"
	"github.com/circleci/ex/system"
)

type Config struct {
	AppName string
	URI     string
	DBName  string
	UseTLS  bool
}

func Load(ctx context.Context, cfg Config, sys *system.System) (*mongo.Database, error) {
	poolMetrics := newMongoPoolMetrics("mongo")
	opts := options.Client().
		ApplyURI(cfg.URI).
		SetAppName(cfg.AppName).
		SetPoolMonitor(poolMetrics.PoolMonitor(nil))

	if cfg.UseTLS {
		opts = opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootcerts.ServerCertPool(),
		})
	}

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	sys.AddCleanup(client.Disconnect)

	sys.AddHealthCheck(&mongoHealth{
		client: client,
	})
	sys.AddMetrics(poolMetrics)

	return client.Database(cfg.DBName), nil
}
