package redisfixture

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t)
	assert.Check(t, fix.Ping(ctx).Err())
	assert.Check(t, fix.DB > 0)
}

func TestSetupAgain(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t)
	assert.Check(t, fix.Ping(ctx).Err())
	assert.Check(t, fix.DB > 0)
}
