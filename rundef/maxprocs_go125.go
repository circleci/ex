//go:build go1.25

package rundef

import (
	"context"
	"runtime"

	"github.com/circleci/ex/o11y"
)

// MaxProcs is a no-op on Go 1.25+
func MaxProcs(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: max procs")
	defer o11y.End(span, &err)

	limit := runtime.GOMAXPROCS(0) // Get the limit the go runtime has determined

	span.AddField("limit", limit)
	return err
}
