package rundef

import (
	"context"
	"fmt"

	"go.uber.org/automaxprocs/maxprocs"

	"github.com/circleci/ex/o11y"
)

func MaxProcs(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: max procs")
	defer o11y.End(span, &err)

	limit, err := maxprocs.Set(maxprocs.Min(1), maxprocs.Logger(func(s string, i ...interface{}) {
		o11y.Log(ctx, fmt.Sprintf(s, i))
	}))
	if err != nil {
		return err
	}

	span.AddField("limit", limit)
	return err
}
