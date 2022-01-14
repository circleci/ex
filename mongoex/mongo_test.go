package mongoex

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestMongoConnection(t *testing.T) {
	ctx := testcontext.Background()
	cfg := Config{
		URI:    "mongodb://root:password@localhost:27107",
		UseTLS: false,
	}

	client, err := New(ctx, "connection-test", cfg)
	assert.NilError(t, err)
	t.Cleanup(func() {
		assert.NilError(t, client.Disconnect(ctx))
	})

	assert.Check(t, cmp.Equal(client.Database("dbname").Name(), "dbname"))
}
