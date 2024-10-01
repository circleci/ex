package rundef

import (
	"context"
	"fmt"
	"runtime"

	"go.uber.org/automaxprocs/maxprocs"

	"github.com/circleci/ex/o11y"
)

func MaxProcs(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: max procs")
	defer o11y.End(span, &err)

	_, err = maxprocs.Set(maxprocs.Min(1), maxprocs.Logger(func(s string, i ...interface{}) {
		// This allows sampling using the rundef namespace which is useful for agents
		prefixed := fmt.Sprintf("rundef: %s", s)

		o11y.Log(ctx, fmt.Sprintf(prefixed, i))
	}))
	if err != nil {
		return err
	}

	limit := runtime.GOMAXPROCS(0)

	span.AddField("limit", limit)
	return err
}
