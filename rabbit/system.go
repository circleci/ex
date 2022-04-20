package rabbit

import (
	"context"

	"github.com/makasim/amqpextra"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/system"
)

type Config struct {
	URL            secret.String
	ConnectionName string
	QueueName      string
}

func Load(ctx context.Context, cfg Config, sys *system.System) (*PublisherPool, error) {
	dialer, err := amqpextra.NewDialer(
		amqpextra.WithURL(cfg.URL.Value()),
		amqpextra.WithConnectionProperties(amqp.Table{
			"connection_name": cfg.ConnectionName,
		}),
	)
	if err != nil {
		return nil, err
	}
	sys.AddCleanup(func(ctx context.Context) error {
		dialer.Close()
		return nil
	})

	pool := NewPublisherPool(ctx, cfg.QueueName, dialer)
	sys.AddCleanup(pool.Close)
	sys.AddMetrics(pool)

	return pool, nil
}
