/*
Package compiler aids with compiling your Go binaries efficiently and in a consistent way.
*/
package compiler

import (
	"context"

	"golang.org/x/sync/errgroup"
)

type Config struct {
	BaseDir     string
	LDFlags     string
	Parallelism int
}

type Parallel struct {
	compiler    *compiler
	parallelism int

	work chan Work
}

func New(cfg Config) *Parallel {
	if cfg.Parallelism <= 0 {
		cfg.Parallelism = 2
	}

	return &Parallel{
		compiler:    newCompiler(cfg.BaseDir, cfg.LDFlags),
		parallelism: cfg.Parallelism,
		work:        make(chan Work, 100),
	}
}

func (t *Parallel) Dir() string {
	return t.compiler.Dir()
}

func (t *Parallel) Cleanup() {
	close(t.work)
}

func (t *Parallel) Add(work Work) {
	if work.Name == "" {
		panic("work.Name not set")
	}
	if work.Target == "" {
		panic("work.Target not set")
	}
	if work.Source == "" {
		panic("work.Source not set")
	}

	if work.Result == nil || *work.Result == "" {
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
