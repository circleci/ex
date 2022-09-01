package dnscache

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestResolver_Resolve_PublicIPs(t *testing.T) {
	ctx := testcontext.Background()

	tests := []struct {
		name string
	}{
		{
			"runner.circleci.com",
		},
		{
			"google.com",
		},
	}

	resolver := New(Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, err := resolver.Resolve(ctx, tt.name)
			assert.Assert(t, err)
			assert.Assert(t, len(ips) > 0)
			for _, ip := range ips {
				assert.Check(t, ip.To4() != nil || ip.To16() != nil)
			}
		})
	}
}

func TestResolver_Resolve_CheckCached(t *testing.T) {
	ctx := testcontext.Background()

	hosts := []string{
		"a.example.com",
		"b.example.com",
		"c.example.com",
		"d.example.com",
	}

	var lookupCount int64
	resolver := New(Config{
		lookupFunc: func(ctx context.Context, r *net.Resolver, host string) ([]net.IP, error) {
			atomic.AddInt64(&lookupCount, 1)
			t.Logf("Got lookup for %q", host)
			for i, h := range hosts {
				if h == host {
					return []net.IP{net.ParseIP(fmt.Sprintf("127.0.0.%d", i+1))}, nil
				}
			}
			return nil, fmt.Errorf("unexpected request: %q", host)
		},
	})

	t.Run("Lookup each host multiple times", func(t *testing.T) {
		for _, host := range hosts {
			for i := 0; i < 20; i++ {
				ips, err := resolver.Resolve(ctx, host)
				assert.Assert(t, err)
				assert.Assert(t, len(ips) > 0)
				for _, ip := range ips {
					assert.Check(t, ip.To4() != nil || ip.To16() != nil)
				}
			}
		}
	})

	t.Run("Check caching happened", func(t *testing.T) {
		assert.Check(t, cmp.Equal(atomic.LoadInt64(&lookupCount), int64(len(hosts))))
	})
}
