package termination

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/circleci/ex/o11y"
)

// ErrTerminated is used to indicate that the Handle func received a shutdown signal.
var ErrTerminated = o11y.NewWarning("terminated")

// Handle is intended to be used with a x/sync/errgroup.WithContext group.
// When a signal is received Handle returns ErrTerminated.
// If the context is cancelled Handle will return with no error.
func Handle(ctx context.Context, delay time.Duration) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-quit:
		o := o11y.FromContext(ctx)
		o.Log(ctx, "system: shutdown signal received", o11y.Field("signal", s),
			o11y.Field("delay", delay))
		time.Sleep(delay)
		return ErrTerminated
	case <-ctx.Done():
		return nil
	}
}
