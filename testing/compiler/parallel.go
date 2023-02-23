package compiler

import (
	"context"
	"os"

	"github.com/circleci/ex/releases/compiler"
)

type Parallel struct {
	compiler *compiler.Parallel
}

func NewParallel(parallelism int) *Parallel {
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		panic(err)
	}

	return &Parallel{
		compiler: compiler.New(compiler.Config{
			BaseDir:     dir,
			LDFlags:     "-w -s",
			Parallelism: parallelism,
		}),
	}
}

func (t *Parallel) Dir() string {
	return t.compiler.Dir()
}

func (t *Parallel) Cleanup() {
	_ = os.RemoveAll(t.compiler.Dir())
}

type Work = compiler.Work

func (t *Parallel) Run(ctx context.Context, work ...Work) error {
	return t.compiler.Run(ctx, work...)
}
