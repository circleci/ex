package redis

import (
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestNew(t *testing.T) {
	ctx := testcontext.Background()

	client1 := New(Options{
		Host: "localhost",
		Port: 6379,
		DB:   1,
	})
	defer client1.Close()

	t.Run("simple connection", func(t *testing.T) {
		err := client1.Ping(ctx).Err()
		assert.Assert(t, err)
	})

	t.Run("connecting to different databases works", func(t *testing.T) {
		t.Run("set key in DB 1", func(t *testing.T) {
			err := client1.Set(ctx, "foo", "bar", 0).Err()
			assert.Assert(t, err)
			b1, err := client1.Get(ctx, "foo").Bytes()
			assert.Assert(t, err)
			assert.Check(t, cmp.Equal("bar", string(b1)))
		})

		t.Run("check key is not found in DB 2", func(t *testing.T) {
			client2 := New(Options{
				Host: "localhost",
				Port: 6379,
				DB:   2,
			})
			defer client2.Close()

			err := client2.Get(ctx, "foo").Err()
			assert.Check(t, errors.Is(err, redis.Nil))
		})
	})
}
