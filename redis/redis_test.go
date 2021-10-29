package redis

import (
	"errors"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/go-redis/redis/v8"

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
		assert.NilError(t, err)
	})

	t.Run("connecting to different databases works", func(t *testing.T) {
		t.Run("set key in DB 1", func(t *testing.T) {
			err := client1.Set(ctx, "foo", "bar", 0).Err()
			assert.NilError(t, err)
			b1, err := client1.Get(ctx, "foo").Bytes()
			assert.NilError(t, err)
			assert.Equal(t, "bar", string(b1))
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
