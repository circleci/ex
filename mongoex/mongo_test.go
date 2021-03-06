package mongoex

import (
	"testing"

	"go.mongodb.org/mongo-driver/mongo/readpref"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/testing/testcontext"
)

func TestNew(t *testing.T) {
	ctx := testcontext.Background()
	cfg := Config{
		URI:    "mongodb://root:password@localhost:27017",
		UseTLS: false,
	}

	client, err := New(ctx, "connection-test", cfg)
	assert.NilError(t, err)
	t.Cleanup(func() {
		t.Run("Disconnect client", func(t *testing.T) {
			err := client.Disconnect(ctx)
			assert.NilError(t, err)
		})
	})

	t.Run("Ping the database", func(t *testing.T) {
		err = client.Ping(ctx, readpref.SecondaryPreferred())
		assert.Assert(t, err)
	})
}

func TestNew_InvalidURLDoesNotLeak(t *testing.T) {
	ctx := testcontext.Background()
	cfg := Config{
		URI:    "mongodb://root:]@localhost:27017",
		UseTLS: false,
	}

	_, err := New(ctx, "connection-test", cfg)
	assert.Error(t, err, "mongoex: failed to parse URI: net/url: invalid userinfo")
}
