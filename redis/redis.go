package redis

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"strconv"

	"github.com/go-redis/redis/v8"

	"github.com/circleci/ex/config/secret"
)

type Options struct {
	Host     string
	Port     int
	User     string
	Password secret.String
	DB       int

	// Optional
	TLS    bool
	CAFunc func() *x509.CertPool
}

// New will only construct a new Redis client with the provided options. It is the caller's
// responsibility to close it at the right time.
func New(o Options) *redis.Client {
	opts := &redis.Options{
		Addr:     net.JoinHostPort(o.Host, strconv.FormatInt(int64(o.Port), 10)),
		Username: o.User,
		Password: o.Password.Value(),
		DB:       o.DB,
	}
	if o.TLS {
		var rootCAs *x509.CertPool
		if o.CAFunc != nil {
			rootCAs = o.CAFunc()
		}

		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: o.Host,
			RootCAs:    rootCAs,
		}
	}

	return redis.NewClient(opts)
}
