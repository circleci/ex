//go:build !go1.25

package rundef

import (
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestMaxProcs(t *testing.T) {
	orig := runtime.GOMAXPROCS(0)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(orig)
	})

	// In order to check the value this sets, we would end up recreating half of the library to fetch the CPU Quota
	// and perform the maths to calculate the limit. So we'll just check we don't error or panic when invoking this
	err := MaxProcs(testcontext.Background())
	assert.NilError(t, err)
}
