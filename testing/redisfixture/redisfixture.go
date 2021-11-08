package redisfixture

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync"
	"testing"

	"github.com/go-redis/redis/v8"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
)

type Fixture struct {
	*redis.Client
	DB int
}

const redisAddr = "localhost:6379"

var (
	once          sync.Once
	databaseCount = uint32(0)
)

func Setup(ctx context.Context, t testing.TB) *Fixture {
	ctx, span := o11y.StartSpan(ctx, "redisfixture: setup")
	defer span.End()

	once.Do(func() {
		readDatabasesCount(ctx, t)
	})

	switch {
	case databaseCount == 0:
		t.Skip("Redis not available")
	case databaseCount < 1000000:
		t.Fatal("not enough Redis databases a unique DB per test, add '--databases 1000000' to Redis setup command")
	}

	// Tests for different go packages are run in parallel by the go test runtime, so
	// we try and use a unique DB for each test.
	db := hash(t.Name(), databaseCount)
	span.AddField("db", db)

	fixClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   db,
	})
	t.Cleanup(func() {
		assert.Check(t, fixClient.Close())
	})

	checkRedisConnection(ctx, t, fixClient)

	err := fixClient.FlushDB(ctx).Err()
	assert.Assert(t, err)

	return &Fixture{
		Client: fixClient,
		DB:     db,
	}
}

func checkRedisConnection(ctx context.Context, t testing.TB, client *redis.Client) {
	err := client.Ping(ctx).Err()
	switch {
	case err != nil && err.Error() == "ERR DB index is out of range":
		assert.Assert(t, err)
	case err != nil:
		t.Skip("Redis not available")
	}
}

func readDatabasesCount(ctx context.Context, t testing.TB) {
	t.Helper()

	setupClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	checkRedisConnection(ctx, t, setupClient)

	res := setupClient.ConfigGet(ctx, "databases")
	assert.Assert(t, res.Err())

	v := res.Val()
	assert.Assert(t, cmp.Len(v, 2))

	dbs, err := strconv.ParseInt(v[1].(string), 10, 64)
	assert.Assert(t, err)

	databaseCount = uint32(dbs)
}

func hash(s string, databaseCount uint32) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(s))
	return int(h.Sum32() % databaseCount)
}
