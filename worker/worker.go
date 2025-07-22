/*
Package worker runs a service worker loop with observability and back-off for no work found.

It is used by various `ex` packages internally, and can be used for any regular work your
service might need to do, such as consuming queue-like data sources.
*/
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"

	"github.com/circleci/ex/o11y"
)

var ErrShouldBackoff = errors.New("should back off")

type Config struct {
	Name string
	// NoWorkBackoff is the backoff strategy to use if the WorkFunc indicates a backoff should happen
	NoWorkBackOff backoff.BackOff
	// MaxWorkTime is the duration after which the context passed to the WorkFunc will be cancelled.
	MaxWorkTime time.Duration
	// MinWorkTime is the minimum duration each work loop can take. The WorkFunc will not be invoked any sooner
	// than the last invocation and MinWorkTime. This can be used to throttle a busy worker.
	MinWorkTime time.Duration
	// WorkFunc should return ErrShouldBackoff if it wants to back off, or set BackoffOnAllErrors
	WorkFunc func(ctx context.Context) error
	// If backoff is desired for any returned error
	BackoffOnAllErrors bool

	waiter func(ctx context.Context, delay time.Duration)
}

// Run a worker, which calls WorkFunc in a loop.
// Run exits when the context is cancelled.
func Run(ctx context.Context, cfg Config) {
	cfg = setDefaults(cfg)
	cfg.NoWorkBackOff.Reset()
	provider := o11y.FromContext(ctx)

	for ctx.Err() == nil {
		start := time.Now()
		wait := doWork(provider, cfg)
		if wait < 0 {
			cfg.NoWorkBackOff.Reset()
			// If the work took longer than the minimum we can continue the loop
			workDuration := time.Since(start)
			if workDuration > cfg.MinWorkTime {
				continue
			}
			// Wait for the minimum work time
			wait = cfg.MinWorkTime - workDuration
		}
		// the default waiter honours context cancellation during the wait
		cfg.waiter(ctx, wait)
	}
}

func setDefaults(cfg Config) Config {
	if cfg.waiter == nil {
		cfg.waiter = wait
	}
	if cfg.NoWorkBackOff == nil {
		cfg.NoWorkBackOff = defaultBackOff()
	}
	return cfg
}

func wait(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func defaultBackOff() backoff.BackOff {
	b := &backoff.ExponentialBackOff{
		InitialInterval: time.Millisecond * 50,
		Multiplier:      2,
		MaxInterval:     time.Second * 5,
	}
	b.Reset()
	return b
}

func doWork(provider o11y.Provider, cfg Config) (backoff time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxWorkTime)
	defer cancel()

	ctx = o11y.WithProvider(ctx, provider)
	ctx, span := provider.StartSpan(ctx, fmt.Sprintf("worker loop: %s", cfg.Name))
	o11y.AddFieldToTrace(ctx, "loop_name", cfg.Name)
	span.AddRawField("meta.type", "worker_loop")

	span.RecordMetric(o11y.Timing("worker_loop", "loop_name", "result"))

	var err error
	defer o11y.End(span, &err)

	// Handle panics so that loop worker behaves like net/http.ServerHTTP
	// https://github.com/golang/go/blob/2566e21/src/net/http/server.go#L79-L85
	defer func() {
		if r := recover(); r != nil {
			err = o11y.HandlePanic(ctx, span, r, nil)
		}

		switch {
		case errors.Is(err, ErrShouldBackoff):
			backoff = cfg.NoWorkBackOff.NextBackOff()
			err = nil
		case cfg.BackoffOnAllErrors && err != nil:
			backoff = cfg.NoWorkBackOff.NextBackOff()
		default:
			// By default, we don't back-off
			backoff = -1
		}

		span.AddField("backoff_ms", backoff.Milliseconds())
	}()

	err = cfg.WorkFunc(ctx)
	return backoff
}
