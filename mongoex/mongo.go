package mongoex

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/rootcerts"
)

type Config struct {
	URI    string
	UseTLS bool

	Options *options.ClientOptions
}

// New connects to mongo. The context passed in is expected to carry an o11y provider
// and is only used for reporting (not for cancellation),
func New(ctx context.Context, appName string, cfg Config) (client *mongo.Client, err error) {
	_, span := o11y.StartSpan(ctx, "cfg: connect to database")
	defer o11y.End(span, &err)

	mongoURL, err := url.Parse(cfg.URI)

	// url.Parse will print the URI if it can't parse. The URI contains the password, so this gets the underlying error
	// without printing the secret string.
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return nil, fmt.Errorf("mongoex: failed to parse URI: %w", urlError.Err)
	} else if err != nil {
		return nil, err
	}

	span.AddField("host", mongoURL.Host)
	span.AddField("username", mongoURL.User)

	opts := cfg.Options
	if opts == nil {
		opts = options.Client()
	}

	opts.
		ApplyURI(cfg.URI).
		SetAppName(appName)

	if cfg.UseTLS {
		opts = opts.SetTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootcerts.ServerCertPool(),
		})
	}

	return mongo.Connect(ctx, opts)
}
