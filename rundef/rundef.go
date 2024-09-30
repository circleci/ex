package rundef

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/o11y"
)

// Defaults configures recommended go runtime options such as GOMEMLIMIT to appropriate values for the detected
// environment
func Defaults(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: defaults")
	defer o11y.End(span, &err)

	eg := errgroup.Group{}
	eg.Go(func() error {
		return MemLimit(ctx)
	})
	eg.Go(func() error {
		return MaxProcs(ctx)
	})

	return eg.Wait()
}
