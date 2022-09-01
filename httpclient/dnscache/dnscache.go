/*
Package dnscache contains a simple in-process cache for DNS lookups
*/
package dnscache

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/vmihailenco/go-tinylfu"
)

const (
	defaultCacheSize = 64
	defaultTTL       = 5 * time.Second
)

func defaultLookupFunc(ctx context.Context, r *net.Resolver, host string) ([]net.IP, error) {
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, len(addrs))
	for i, ia := range addrs {
		ips[i] = ia.IP
	}

	return ips, nil
}

type Resolver struct {
	config Config

	// mutable state
	mu    sync.Mutex
	cache *tinylfu.T
}

type Config struct {
	CacheSize int
	TTL       time.Duration

	// Resolver optionally allows specifying a custom resolver
	Resolver *net.Resolver

	lookupFunc func(ctx context.Context, r *net.Resolver, host string) ([]net.IP, error)
}

func New(c Config) *Resolver {
	if c.CacheSize == 0 {
		c.CacheSize = defaultCacheSize
	}

	if c.TTL == 0 {
		c.TTL = defaultTTL
	}

	if c.Resolver == nil {
		c.Resolver = net.DefaultResolver
	}

	if c.lookupFunc == nil {
		c.lookupFunc = defaultLookupFunc
	}

	return &Resolver{
		config: c,
		cache:  tinylfu.New(c.CacheSize, 100000),
	}
}

func (r *Resolver) Resolve(ctx context.Context, addr string) ([]net.IP, error) {
	v, ok := r.cacheGet(addr)
	if ok {
		return v.([]net.IP), nil
	}

	ips, err := r.config.lookupFunc(ctx, r.config.Resolver, addr)
	if err != nil {
		return nil, err
	}

	r.cacheSet(&tinylfu.Item{
		Key:      addr,
		Value:    ips,
		ExpireAt: time.Now().Add(r.config.TTL),
	})
	return ips, nil
}

func (r *Resolver) cacheSet(item *tinylfu.Item) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache.Set(item)
}

func (r *Resolver) cacheGet(addr string) (interface{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.cache.Get(addr)
}
