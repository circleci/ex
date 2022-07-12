package compiler

import (
	"context"
	"io/ioutil"
	"os"

	"github.com/circleci/ex/releases/compiler"
)

type Parallel struct {
	compiler *compiler.Parallel
}

func NewParallel(parallelism int) *Parallel {
	dir, err := ioutil.TempDir("", "")
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
	t.compiler.Cleanup()
	_ = os.RemoveAll(t.compiler.Dir())
}

type Work = compiler.Work

func (t *Parallel) Add(work Work) {
	t.compiler.Add(work)
}

func (t *Parallel) Run(ctx context.Context) error {
	return t.compiler.Run(ctx)
}
