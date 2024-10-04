package rundef

import (
	"context"
	"errors"

	"github.com/circleci/ex/o11y"
)

// Defaults configures recommended go runtime options such as GOMEMLIMIT to appropriate values for the detected
// environment
func Defaults(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: defaults")
	defer o11y.End(span, &err)

	err = errors.Join(err,
		MemLimit(ctx),
		MaxProcs(ctx),
	)
	
	return err
}
