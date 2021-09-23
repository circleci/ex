package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"strconv"

	"github.com/circleci/ex/system"

	"github.com/go-redis/redis/v8"

	"github.com/circleci/ex/config/secret"
)

type Options struct {
	Host     string
	Port     int
	User     string
	Password secret.String

	// Optional
	TLS    bool
	CAFunc func() *x509.CertPool
}

// Load will create a new Redis client, and wire it into the provided System with
// default lifecycle management and observability.
func Load(o Options, sys *system.System) *redis.Client {
	client := New(o)

	sys.AddCleanup(func(_ context.Context) error {
		return client.Close()
	})

	sys.AddHealthCheck(NewHealthCheck(client))
	sys.AddMetrics(NewMetrics("redis", client))

	return client
}

// New will only construct a new Redis client with the provided options. It is the caller's
// responsibility to close it at the right time.
func New(o Options) *redis.Client {
	opts := &redis.Options{
		Addr:     net.JoinHostPort(o.Host, strconv.FormatInt(int64(o.Port), 10)),
		Username: o.User,
		Password: o.Password.Value(),
	}
	if o.TLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: o.Host,
			RootCAs:    o.CAFunc(),
		}
	}

	return redis.NewClient(opts)
}
