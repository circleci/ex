package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"gotest.tools/v3/assert/cmp"

	"github.com/cenkalti/backoff/v4"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

func TestRunWorkerLoop_SleepsAfterNoWorkCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	counter := 0
	expected := 10
	f := func(ctx context.Context) error {
		counter++
		if counter == expected {
			cancel()
		}
		return ErrShouldBackoff
	}

	waitCalls := 0
	waiter := func(_ context.Context, delay time.Duration) {
		waitCalls++
	}

	backOff := new(fakeBackOff)
	Run(ctx, Config{
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, expected))
	assert.Check(t, cmp.Equal(waitCalls, expected))
	assert.Check(t, cmp.Equal(backOff.resetCallCount, 1),
		"reset should only be called once to initialize it")

}

type fakeBackOff struct {
	nextBackOff    time.Duration
	nextCallCount  int
	resetCallCount int
}

func (b *fakeBackOff) NextBackOff() time.Duration {
	b.nextCallCount++
	return b.nextBackOff
}

func (b *fakeBackOff) Reset() {
	b.resetCallCount++
}

var _ backoff.BackOff = &fakeBackOff{}

func TestRunWorkerLoop_DoesNotSleepAfterWorkCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	counter := 0
	expected := 3
	f := func(ctx context.Context) error {
		counter++
		if counter == expected {
			cancel()
		}
		return nil
	}

	waiter := func(_ context.Context, delay time.Duration) {
		panic("wait should never be called")
	}

	backOff := new(fakeBackOff)
	Run(ctx, Config{
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, 0))
	// Reset is called once to initialize the backOff
	assert.Check(t, cmp.Equal(backOff.resetCallCount, expected+1))
}

func TestRunWorkerLoop_DoesNotSleepAfterOtherErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	counter := 0
	expected := 3
	f := func(ctx context.Context) error {
		counter++
		if counter == expected {
			cancel()
		}
		return errors.New("something went horribly wrong")
	}

	waiter := func(_ context.Context, delay time.Duration) {
		panic("wait should never be called")
	}

	backOff := new(fakeBackOff)
	Run(ctx, Config{
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, 0))
	// Reset is called once to initialize the backOff
	assert.Check(t, cmp.Equal(backOff.resetCallCount, expected+1))
}

func TestRunWorkerLoop_ExitsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	ran := make(chan struct{})
	go func() {
		Run(ctx, Config{
			WorkFunc: func(ctx context.Context) error {
				calls++
				// since we return no error, Run will call this in a tight loop
				// we should have got a few calls at least
				time.Sleep(time.Millisecond)
				return nil
			},
		})
		close(ran)
	}()

	// cancel after a short delay so we almost certainly did some calls
	time.Sleep(time.Millisecond * 100)
	cancel()

	select {
	case <-ran:
	case <-time.After(time.Second):
		// given that we cancelled after .1 sec if it took this long for
		// the context cancellation of Run to be noticed then something is very wrong.
		t.Fatal("run did not finish in time")
	}

	assert.Check(t, calls > 1)
}

func TestDoWork_WorkFuncPanics(t *testing.T) {
	f := func(ctx context.Context) error {
		panic("Oops")
	}

	ctx := context.Background()
	provider := o11y.FromContext(ctx)
	cfg := Config{WorkFunc: f}
	assert.Check(t, doWork(provider, cfg) < 0)
}
