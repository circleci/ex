package termination

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/circleci/ex/o11y"
)

// ErrTerminated is used to indicate that the errgroup should cancel the
// context, but that the shutdown is due to an expected signal, not an unhandled
// error.
var ErrTerminated = o11y.NewWarning("terminated")

// Handle is intended to be used with a x/sync/errgroup.WithContext group.
// When a signal is received signalHandler returns an error. When the errgroup
// receives the error it will cancel the context. All long running operations
// should terminate once the context is canceled.
func Handle(ctx context.Context) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-quit:
		o := o11y.FromContext(ctx)
		o.Log(ctx, "system: shutdown signal received", o11y.Field("signal", s))
		return ErrTerminated
	case <-ctx.Done():
		return nil
	}
}
