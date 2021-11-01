package redisfixture

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func Test_relativePackageName(t *testing.T) {
	assert.Check(t, cmp.Equal(relativePackageName(1), "/testing/redisfixture"))
}

func Test_packageName(t *testing.T) {
	assert.Check(t, cmp.Equal(packageName(0), "github.com/circleci/ex/testing/redisfixture"))
}

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t)
	assert.Check(t, fix.Ping(ctx).Err())
	assert.Check(t, fix.DB > 0)
}
