package compiler

import (
	"context"

	"golang.org/x/sync/errgroup"
)

type Parallel struct {
	compiler    *Compiler
	parallelism int

	work chan Work
}

func NewParallel(parallelism int) *Parallel {
	return &Parallel{
		compiler:    New(),
		parallelism: parallelism,
		work:        make(chan Work, 100),
	}
}

func (t *Parallel) Dir() string {
	return t.compiler.Dir()
}

func (t *Parallel) Cleanup() {
	defer t.compiler.Cleanup()
	close(t.work)
}

func (t *Parallel) Add(work Work) {
	if work.Result == nil {
		panic("work.Result not set")
	}
	if work.Name == "" {
		panic("work.Name not set")
	}
	if work.Target == "" {
		panic("work.Target not set")
	}
	if work.Source == "" {
		panic("work.Source not set")
	}

	if *work.Result == "" {
		t.work <- work
	}
}

func (t *Parallel) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	for i := 0; i < t.parallelism; i++ {
		g.Go(func() error {
			for {
				select {
				case w := <-t.work:
					_, err := t.compiler.Compile(ctx, w)
					if err != nil {
						return err
					}
				default:
					return nil
				}
			}
		})
	}
	return g.Wait()
}
