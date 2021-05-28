package termination

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

var ErrTerminated = errors.New("terminated")

func Handle(ctx context.Context) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		return ErrTerminated
	case <-ctx.Done():
		return nil
	}
}
