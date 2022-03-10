package dnscache

import (
	"context"
	"math/rand"
	"net"
	"time"
)

type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func DialContext(resolver *Resolver, baseDial DialFunc) DialFunc {
	if baseDial == nil {
		baseDial = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		h, p, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := resolver.Resolve(ctx, h)
		if err != nil {
			return nil, err
		}

		var firstErr error
		for _, randomIndex := range randPerm(len(ips)) {
			conn, err := baseDial(ctx, network, net.JoinHostPort(ips[randomIndex].String(), p))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}

		return nil, firstErr
	}
}

var randPerm = func(n int) []int {
	return rand.Perm(n)
}
