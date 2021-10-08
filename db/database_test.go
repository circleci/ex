package db

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/testcontext"
)

func TestDB(t *testing.T) {
	ctx := testcontext.Background()
	db, err := New(ctx, "the-db-name", "the-app-name", Config{
		Host: "localhost",
		Port: 5432,
		User: "user",
		Pass: "password",
		Name: "dbname",
	})
	assert.Assert(t, err)

	txm := NewTxManager(db)

	const query = `SELECT feature_id FROM information_schema.sql_features;`

	t.Run("With transaction", func(t *testing.T) {
		ctx, span := o11y.StartSpan(ctx, "with-tx")
		defer span.End()

		var res []string
		err = txm.WithTx(ctx, func(ctx context.Context, q Querier) error {
			return q.SelectContext(ctx, &res, query)
		})
		assert.Assert(t, err)
		assert.Check(t, cmp.Len(res, 712))
	})

	t.Run("No transaction", func(t *testing.T) {
		ctx, span := o11y.StartSpan(ctx, "no-tx")
		defer span.End()

		var res []string
		err = txm.NoTx().SelectContext(ctx, &res, query)
		assert.Assert(t, err)
		assert.Check(t, cmp.Len(res, 712))
	})
}
