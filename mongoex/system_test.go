package mongoex

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

func TestLoad(t *testing.T) {
	ctx := testcontext.Background()
	cfg := Config{
		URI:    "mongodb://root:password@localhost:27017",
		UseTLS: false,
	}

	sys := system.New()
	defer sys.Cleanup(ctx)

	client, err := Load(ctx, "connection-test", cfg, sys)
	assert.Assert(t, err)
	t.Cleanup(func() {
		t.Run("Check client is already disconnected", func(t *testing.T) {
			err := client.Disconnect(ctx)
			assert.Check(t, errors.Is(err, mongo.ErrClientDisconnected))
		})
	})

	t.Run("Ping the database", func(t *testing.T) {
		err = client.Ping(ctx, readpref.SecondaryPreferred())
		assert.Assert(t, err)
	})
}
