package rundef

import (
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/skip"

	"github.com/circleci/ex/testing/testcontext"
)

func TestMemLimit(t *testing.T) {
	skip.If(t, runtime.GOOS != "linux", "this test relies on cgroups")

	orig := debug.SetMemoryLimit(-1) // restore original mem limit
	t.Cleanup(func() {
		debug.SetMemoryLimit(orig)
	})

	limit, err := memlimit.FromCgroup()
	assert.NilError(t, err)

	// 90% of the limit. This mirrors the calculation in the automemlimit lib, so that rounding differences
	// cannot cause test flakes
	expected := int64(float64(limit) * 0.9)

	err = MemLimit(testcontext.Background())
	assert.NilError(t, err)

	actual := debug.SetMemoryLimit(-1)
	assert.Check(t, cmp.Equal(actual, expected))
}
