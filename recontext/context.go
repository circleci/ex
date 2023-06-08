// Package recontext provides means of obtaining a derived context which ignores parent's deadline, timeout, and
// cancellation.
package recontext

import (
	"context"
	"time"
)

// Context that wraps another and suppresses its deadline or cancellation.
// Suppression works by redefining error and timeout/deadline methods for this context impl, however
// the struct itself is never handed out, and instead is wrapped in a standard context from the context
// library, which adds a separate deadline/timeout to prevent stuck contexts.
type valueOnlyContext struct{ context.Context }

// WithNewDeadline returns a derived context that will ignore cancellation, deadline, and timeout of the parent context.
// In order to avoid stuck contexts, new deadline is mandatory.
func WithNewDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(&valueOnlyContext{parent}, deadline)
}

// WithNewTimeout returns a derived context that will ignore cancellation, deadline, and timeout of the parent context.
// In order to avoid stuck contexts, new timeout is mandatory.
func WithNewTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(&valueOnlyContext{parent}, timeout)
}

func (valueOnlyContext) Deadline() (deadline time.Time, ok bool) { return time.Time{}, false }
func (valueOnlyContext) Done() <-chan struct{}                   { return nil }
func (valueOnlyContext) Err() error                              { return nil }
