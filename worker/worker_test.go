package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/testcontext"
)

func TestRun_SleepsAfterNoWorkCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
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
		Name:          "sleep-after-no-work",
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, expected))
	assert.Check(t, cmp.Equal(waitCalls, expected))
	assert.Check(t, cmp.Equal(backOff.resetCallCount, 1),
		"reset should only be called once to initialize it")

}
func TestRun_SleepsAnyErrorWhenConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
	counter := 0
	expected := 10
	f := func(ctx context.Context) error {
		counter++
		if counter == expected {
			cancel()
		}
		return errors.New("a custom error")
	}

	waitCalls := 0
	waiter := func(_ context.Context, delay time.Duration) {
		waitCalls++
	}

	backOff := new(fakeBackOff)
	Run(ctx, Config{
		Name:               "sleep-any-error-when-configured",
		BackoffOnAllErrors: true,

		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, expected))
	assert.Check(t, cmp.Equal(waitCalls, expected))
	assert.Check(t, cmp.Equal(backOff.resetCallCount, 1),
		"reset should only be called once to initialize it")

}

func TestRun_DoesNotSleepAfterWorkCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
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
		Name:          "does-not-sleep-after-work-cycle",
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, 0))
	// Reset is called once to initialize the backOff
	assert.Check(t, cmp.Equal(backOff.resetCallCount, expected+1))
}

func TestRun_DoesNotSleepAfterOtherErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())
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
		Name:          "does-not-sleep-after-other-errors",
		NoWorkBackOff: backOff,
		WorkFunc:      f,
		waiter:        waiter,
	})

	assert.Check(t, cmp.Equal(backOff.nextCallCount, 0))
	// Reset is called once to initialize the backOff
	assert.Check(t, cmp.Equal(backOff.resetCallCount, expected+1))
}

func TestRun_ExitsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(testcontext.Background())

	calls := 0
	ran := make(chan struct{})
	go func() {
		Run(ctx, Config{
			Name: "exits-when-context-is-cancelled",
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

func Test_doWork_WorkFuncPanics(t *testing.T) {
	f := func(ctx context.Context) error {
		panic("Oops")
	}

	ctx := testcontext.Background()
	provider := o11y.FromContext(ctx)
	cfg := Config{
		Name:     "work-func-panics",
		WorkFunc: f,
	}
	assert.Check(t, doWork(provider, cfg) < 0)
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
