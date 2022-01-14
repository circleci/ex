package mongo

import (
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

func TestMongoConnection(t *testing.T) {
	ctx := testcontext.Background()
	cfg := Config{
		AppName: "connection-test",
		URI:     "mongodb://root:password@localhost:27107",
		DBName:  "dbname",
		UseTLS:  false,
	}

	sys := system.New()
	db, err := Load(ctx, cfg, sys)
	assert.NilError(t, err)

	assert.Check(t, cmp.Equal(db.Name(), "dbname"))
	assert.Check(t, cmp.Equal(len(sys.HealthChecks()), 1))
}
