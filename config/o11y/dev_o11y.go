package o11y

import (
	"context"
	"sync"

	"github.com/circleci/ex/o11y"
)

// DevInit is some crazy hackery to handle the fact that the beeline lib uses a
// singleton shared client that we cant simply guard with sync once, since we are not
// coordinating the calls to close. Instead we route the setup through here where we
// can create the provider once and coordinate the closes.
// Other Approaches:
// 1. We could protect beeline init (from the races) and then not close beeline in dev mode
// or sync once the close call but that would still mean doing something like this interceptor.
//
// 2. A stand alone development stack launcher of some sort. This would probably be the most correct
// in terms of running like production, but it adds some complexity to developer testing.
//
// 3. Instead of this mess we would have to implement beeline ourselves in a way that has a single client.
// we could do that and attempt to upstream the PR, but this is a fair amount of effort.
//
// This small wrapper is only in the code init flow, and once seen and understood can be forgotten,
// so it was considered OK to keep for now.
//
// If the coordinator is nil (for example if DevInit is not called as per production) then we immediately
// defer to the real setup
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
