package o11y

import (
	"context"
	"sync"

	"github.com/circleci/ex/o11y"
)

// DevInit needs to be called to construct the coordinator (it is best to avoid init())
// If this is not called we will panic early. This is expected to only be called in development testing
func DevInit() {
	coordinator = &closeCoord{}
}

var coordinator *closeCoord

// coordinator ensures we only init once (to satisfy the race detector) and
// ensures we only call the real close once, this allows us to use the same signature for
// Setup above, so this is all
type closeCoord struct {
	mu sync.Mutex

	closes    int
	provider  o11y.Provider
	realClose func(context.Context)
}

func (c *closeCoord) setup(ctx context.Context, o Config) (context.Context, func(context.Context), error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closes == 0 {
		ctx, cleanup, err := setup(ctx, o)
		if err != nil {
			return ctx, nil, err
		}
		coordinator.realClose = cleanup
		coordinator.provider = o11y.FromContext(ctx)
		c.closes++
		return ctx, coordinator.close, nil
	}

	ctx = o11y.WithProvider(ctx, coordinator.provider)
	c.closes++
	return ctx, coordinator.close, nil
}

func (c *closeCoord) close(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closes--
	if c.closes == 0 {
		c.realClose(ctx)
	}
}
