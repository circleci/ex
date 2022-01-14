package redisfixture

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t, Connection{Addr: "localhost:6379"})
	t.Run("Check we can ping", func(t *testing.T) {
		assert.Check(t, fix.Ping(ctx).Err())
	})
	t.Run("Check we got a non zero DB", func(t *testing.T) {
		assert.Check(t, fix.DB > 0)
	})
}

func TestSetupAgain(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t, Connection{Addr: "localhost:6379"})
	t.Run("Check we can ping", func(t *testing.T) {
		assert.Check(t, fix.Ping(ctx).Err())
	})
	t.Run("Check we got a non zero DB", func(t *testing.T) {
		assert.Check(t, fix.DB > 0)
	})
}
