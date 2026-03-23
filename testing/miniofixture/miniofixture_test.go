package miniofixture

import (
	"testing"

	"github.com/circleci/ex/testing/testcontext"
)

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()
	Default(ctx, t)
}
