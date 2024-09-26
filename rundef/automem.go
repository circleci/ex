package rundef

import (
	"context"

	"github.com/KimMachineGun/automemlimit/memlimit"

	"github.com/circleci/ex/o11y"
)

// MemLimit sets the GOMEMLIMIT to the recommended default of 90% of the memory available. It attempts to calculate this
// from the process cgroup first and will fall back to the total system memory if cgroups is not available.
func MemLimit(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "rundef: mem limit")
	defer o11y.End(span, &err)

	limit, err := memlimit.SetGoMemLimitWithOpts(
		memlimit.WithRatio(0.9),
		memlimit.WithProvider(
			memlimit.ApplyFallback(
				memlimit.FromCgroup,
				memlimit.FromSystem,
			)))
	if err != nil {
		return err
	}
	span.AddField("limit", limit)
	return nil
}
