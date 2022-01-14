package mongoex

import (
	"context"
	"crypto/tls"
	"net/url"

	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/rootcerts"
)

type Config struct {
	URI    string
	UseTLS bool

	PoolMonitor *event.PoolMonitor
}

// New connects to mongo. The context passed in is expected to carry an o11y provider
// and is only used for reporting (not for cancellation),
func New(ctx context.Context, appName string, cfg Config) (client *mongo.Client, err error) {
	_, span := o11y.StartSpan(ctx, "cfg: connect to database")
	defer o11y.End(span, &err)

	mongoURL, err := url.Parse(cfg.URI)
	if err != nil {
		return nil, err
	}

	span.AddField("host", mongoURL.Host)
	span.AddField("username", mongoURL.User)

	opts := options.Client().
		ApplyURI(cfg.URI).
		SetAppName(appName)

	if cfg.PoolMonitor != nil {
		opts.SetPoolMonitor(cfg.PoolMonitor)
	}

	if cfg.UseTLS {
		opts = opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootcerts.ServerCertPool(),
		})
	}

	client, err = mongo.Connect(ctx, opts)

	return client, err
}
