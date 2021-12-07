package valueonly

import (
	"context"
	"time"
)

// Context that wraps another and suppresses its deadline or cancellation.
type Context struct{ context.Context }

func (Context) Deadline() (deadline time.Time, ok bool) { return }
func (Context) Done() <-chan struct{}                   { return nil }
func (Context) Err() error                              { return nil }
