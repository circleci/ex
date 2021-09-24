package db

import (
	"context"

	"github.com/circleci/ex/system"
)

func Load(ctx context.Context, dbName, appName string, cfg Config, sys *system.System) (*TxManager, error) {
	db, err := New(ctx, dbName, appName, cfg)
	if err != nil {
		return nil, err
	}

	dbCheck := &HealthCheck{Name: dbName + "-db", DB: db}
	sys.AddMetrics(dbCheck)
	sys.AddHealthCheck(dbCheck)
	sys.AddCleanup(func(ctx context.Context) error {
		return db.Close()
	})

	return NewTxManager(db), nil
}
