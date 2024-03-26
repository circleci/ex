package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/circleci/ex/config/secret"
)

type ClusterOptions struct {
	// Name of the client for metrics and health check, default is "redis"
	Name string

	// A seed list of host:port addresses of cluster nodes.
	Addrs []string

	// The maximum number of retries before giving up. Command is retried
	// on network errors and MOVED/ASK redirects.
	// Default is 3 retries.
	MaxRedirects int

	// Enables read-only commands on slave nodes.
	ReadOnly bool
	// Allows routing read-only commands to the closest master or slave node.
	// It automatically enables ReadOnly.
	RouteByLatency bool
	// Allows routing read-only commands to the random master or slave node.
	// It automatically enables ReadOnly.
	RouteRandomly bool

	// Optional function that returns cluster slots information.
	// It is useful to manually create cluster of standalone Redis servers
	// and load-balance read/write operations between master and slaves.
	// It can use service like ZooKeeper to maintain configuration information
	// and Cluster.ReloadState to manually trigger state reloading.
	ClusterSlots func(context.Context) ([]redis.ClusterSlot, error)

	// Use the specified User to authenticate the current connection
	// with one of the connections defined in the ACL list when connecting
	// to a Redis 6.0 instance, or greater, that is using the Redis ACL system.
	User string
	// Optional password. Must match the password specified in the
	// requirepass server configuration option (if connecting to a Redis 5.0 instance, or lower),
	// or the User Password when connecting to a Redis 6.0 instance, or greater,
	// that is using the Redis ACL system.
	Password secret.String

	// Optional

	// Maximum number of retries before giving up.
	// Default is 3 retries; -1 (not 0) disables retries.
	MaxRetries int
	// Minimum backoff between each retry.
	// Default is 8 milliseconds; -1 disables backoff.
	MinRetryBackoff time.Duration
	// Maximum backoff between each retry.
	// Default is 512 milliseconds; -1 disables backoff.
	MaxRetryBackoff time.Duration

	// Dial timeout for establishing new connections.
	// Default is 5 seconds.
	DialTimeout time.Duration
	// Timeout for socket reads. If reached, commands will fail
	// with a timeout instead of blocking. Use value -1 for no timeout and 0 for default.
	// Default is 3 seconds.
	ReadTimeout time.Duration
	// Timeout for socket writes. If reached, commands will fail
	// with a timeout instead of blocking.
	// Default is ReadTimeout.
	WriteTimeout time.Duration

	// Type of connection pool.
	// true for FIFO pool, false for LIFO pool.
	// Note that fifo has higher overhead compared to lifo.
	PoolFIFO bool
	// Maximum number of socket connections.
	// Default is 10 connections per every available CPU as reported by runtime.GOMAXPROCS.
	PoolSize int
	// Minimum number of idle connections which is useful when establishing
	// new connection is slow.
	MinIdleConns int
	// Connection age at which client retires (closes) the connection.
	// Default is to not close aged connections.
	ConnMaxLifetime time.Duration
	// Amount of time client waits for connection if all connections
	// are busy before returning an error.
	// Default is ReadTimeout + 1 second.
	PoolTimeout time.Duration
	// Amount of time after which client closes idle connections.
	// Should be less than server's timeout.
	// Default is 5 minutes. -1 disables idle timeout check.
	ConnMaxIdleTime time.Duration

	TLS    bool
	CAFunc func() *x509.CertPool
}

// NewCluster will only construct a new Redis client with the provided options. It is the caller's
// responsibility to close it at the right time.
func NewCluster(o ClusterOptions) *redis.ClusterClient {
	opts := &redis.ClusterOptions{
		Addrs:          o.Addrs,
		MaxRedirects:   o.MaxRedirects,
		ReadOnly:       o.ReadOnly,
		RouteByLatency: o.RouteByLatency,
		RouteRandomly:  o.RouteRandomly,
		ClusterSlots:   o.ClusterSlots,

		Username: o.User,
		Password: o.Password.Raw(),

		MaxRetries:      o.MaxRetries,
		MinRetryBackoff: o.MinRetryBackoff,
		MaxRetryBackoff: o.MaxRetryBackoff,
		DialTimeout:     o.DialTimeout,
		ReadTimeout:     o.ReadTimeout,
		WriteTimeout:    o.WriteTimeout,
		PoolFIFO:        o.PoolFIFO,
		PoolSize:        o.PoolSize,
		MinIdleConns:    o.MinIdleConns,
		ConnMaxLifetime: o.ConnMaxLifetime,
		PoolTimeout:     o.PoolTimeout,
		ConnMaxIdleTime: o.ConnMaxIdleTime,
	}
	if o.TLS {
		var rootCAs *x509.CertPool
		if o.CAFunc != nil {
			rootCAs = o.CAFunc()
		}

		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootCAs,
		}
	}

	return redis.NewClusterClient(opts)
}
