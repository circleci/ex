package redis

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestNew(t *testing.T) {
	ctx := testcontext.Background()

	client := New(Options{
		Host: "localhost",
		Port: 6379,
	})

	err := client.Ping(ctx).Err()
	assert.Check(t, err)
}
