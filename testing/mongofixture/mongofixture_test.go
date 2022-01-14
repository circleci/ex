package mongofixture

import (
	"testing"

	"go.mongodb.org/mongo-driver/mongo/readpref"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

func TestSetup(t *testing.T) {
	ctx := testcontext.Background()
	fix := Setup(ctx, t, Connection{URI: "mongodb://root:password@localhost:27017"})

	t.Run("Check we got some kind of connection", func(t *testing.T) {
		assert.Assert(t, fix.DB != nil)
		assert.Check(t, cmp.Contains(fix.Name, "-TestSetup"))
	})

	t.Run("Ping the database", func(t *testing.T) {
		err := fix.DB.Client().Ping(ctx, readpref.Secondary())
		assert.Check(t, err)
	})
}
