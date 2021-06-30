package worker

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"

	"github.com/circleci/ex/o11y"
)

var ErrShouldBackoff = errors.New("should back off")

type Config struct {
	Name          string
	NoWorkBackOff backoff.BackOff
	MaxWorkTime   time.Duration
	// WorkFunc should return ErrShouldBackoff if it wants the loop to begin backing off
	WorkFunc func(ctx context.Context) error
	waiter   func(ctx context.Context, delay time.Duration)
}

// Run a worker, which calls WorkFunc in a loop.
// Run exits when the context is cancelled.
func Run(ctx context.Context, cfg Config) {
	cfg = setDefaults(cfg)
	cfg.NoWorkBackOff.Reset()
	provider := o11y.FromContext(ctx)

	for ctx.Err() == nil {
		wait := doWork(provider, cfg)
		if wait < 0 {
			cfg.NoWorkBackOff.Reset()
			continue
		}
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
		MaxElapsedTime:  0,
		Clock:           backoff.SystemClock,
	}
	b.Reset()
	return b
}

func doWork(provider o11y.Provider, cfg Config) (backoff time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxWorkTime)
	defer cancel()

	ctx = o11y.WithProvider(ctx, provider)
	ctx, span := provider.StartSpan(ctx, "worker loop: "+cfg.Name)
	span.RecordMetric(o11y.Timing("worker_loop", "loop_name", "result"))
	span.AddField("loop_name", cfg.Name)
	var err error
	defer o11y.End(span, &err)

	// Handle panics so that loop worker behaves like net/http.ServerHTTP
	// https://github.com/golang/go/blob/2566e21/src/net/http/server.go#L79-L85
	defer func() {
		if r := recover(); r != nil {
			err = o11y.HandlePanic(ctx, span, r, nil)
		}
	}()

	// By default, we don't back-off
	backoff = -1
	err = cfg.WorkFunc(ctx)
	if errors.Is(err, ErrShouldBackoff) {
		backoff = cfg.NoWorkBackOff.NextBackOff()
		err = nil
	}

	span.AddField("backoff_ms", backoff.Milliseconds())
	return backoff
}
