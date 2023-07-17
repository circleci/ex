package dbfixture

import (
	_ "embed"
	"errors"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/db"
	"github.com/circleci/ex/testing/testcontext"
)

var (
	//go:embed testdata/schema.sql
	schema string

	//go:embed testdata/appUserSchema.sql
	appUserSchema string

	conn = Connection{
		Host:     "localhost:5432",
		User:     "user",
		Password: "password",
	}
)

func TestSetupDB_Isolation(t *testing.T) {
	ctx := testcontext.Background()
	fix1 := SetupDB(ctx, t, schema, conn)
	fix2 := SetupDB(ctx, t, schema, conn)

	t.Run("insert data into db1", func(t *testing.T) {
		// language=PostgreSQL
		_, err := fix1.TX.NoTx().ExecContext(ctx, `INSERT INTO test_table (id, name) values ('123', 'apple');`)
		assert.Assert(t, err)
	})

	t.Run("check data is in db1", func(t *testing.T) {
		var ids []string
		// language=PostgreSQL
		err := fix1.TX.NoTx().SelectContext(ctx, &ids, `SELECT id FROM test_table;`)
		assert.Assert(t, err)
		assert.Check(t, cmp.DeepEqual([]string{"123"}, ids))
	})

	t.Run("check data is not in db2", func(t *testing.T) {
		var ids []string
		// language=PostgreSQL
		err := fix2.TX.NoTx().SelectContext(ctx, &ids, `SELECT id FROM test_table;`)
		assert.Assert(t, errors.Is(err, db.ErrNop))
	})
}

func TestReset(t *testing.T) {
	ctx := testcontext.Background()
	fix := SetupDB(ctx, t, schema, conn)

	t.Run("insert data into db1", func(t *testing.T) {
		// language=PostgreSQL
		_, err := fix.TX.NoTx().ExecContext(ctx, `INSERT INTO test_table (id, name) values ('123', 'apple');`)
		assert.Assert(t, err)
	})

	t.Run("check data is in db1", func(t *testing.T) {
		var ids []string
		// language=PostgreSQL
		err := fix.TX.NoTx().SelectContext(ctx, &ids, `SELECT id FROM test_table;`)
		assert.Assert(t, err)
		assert.Check(t, cmp.DeepEqual([]string{"123"}, ids))
	})

	t.Run("reset the DB", func(t *testing.T) {
		err := fix.Reset(ctx)
		assert.Assert(t, err)
	})

	t.Run("check data is not in db", func(t *testing.T) {
		var ids []string
		// language=PostgreSQL
		err := fix.TX.NoTx().SelectContext(ctx, &ids, `SELECT id FROM test_table;`)
		assert.Assert(t, errors.Is(err, db.ErrNop))
	})
}

func TestSetupDB_AppUser(t *testing.T) {
	ctx := testcontext.Background()

	fix := SetupDB(ctx, t, appUserSchema, Connection{
		Host:     conn.Host,
		User:     conn.User,
		Password: conn.Password,

		// Least-privilege app user values
		AppUser:     "test_role_1",
		AppPassword: "teehee",
	})
	_ = fix.Reset(ctx)

	t.Run("fails to create db (no grant)", func(t *testing.T) {
		_, err := fix.TX.NoTx().ExecContext(ctx, `CREATE DATABASE foo;`)
		assert.ErrorContains(t, err, "permission denied")
	})

	t.Run("fails to insert (no grant)", func(t *testing.T) {
		_, err := fix.TX.NoTx().ExecContext(ctx, `INSERT INTO test_table (id, name) VALUES (123, 'banana');`)
		assert.ErrorContains(t, err, "permission denied")
	})

	t.Run("can select (nothing)", func(t *testing.T) {
		var res []string
		err := fix.TX.NoTx().SelectContext(ctx, &res, `SELECT name FROM test_table;`)
		assert.ErrorIs(t, err, db.ErrNop)
	})

}
