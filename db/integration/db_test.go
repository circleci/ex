package integration

import (
	"context"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/circleci/ex/db"
	"github.com/circleci/ex/testing/dbfixture"
	"github.com/circleci/ex/testing/testcontext"
)

func TestDB(t *testing.T) {
	ctx := testcontext.Background()
	conn := dbfixture.Connection{
		Host:     "localhost:5432",
		User:     "user",
		Password: "password",
	}
	fix := dbfixture.SetupDB(ctx, t,
		"CREATE TABLE peeps (id text PRIMARY KEY, name text, height smallint, dob timestamp);", conn)

	t.Run("statements", func(t *testing.T) {
		type person struct {
			ID     string
			Name   string
			Height int
			DOB    time.Time
		}
		// add a person
		person1 := person{
			ID:     "id1",
			Name:   "bob",
			Height: 187,
			DOB:    time.Date(1998, 7, 4, 0, 0, 0, 0, time.UTC),
		}
		err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {
			const sql = "INSERT INTO peeps (id,name,height,dob) VALUES (:id,:name,:height,:dob);"
			_, err := q.NamedExecContext(ctx, sql, person1)

			return err
		})
		assert.Assert(t, err)

		t.Run("get", func(t *testing.T) {
			p := person{}
			err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {
				return q.GetContext(ctx, &p, "SELECT * from peeps WHERE id=$1", "id1")
			})
			assert.Assert(t, err)
			assert.DeepEqual(t, p, person1)
		})

		t.Run("get named", func(t *testing.T) {
			p := person{}
			err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {
				pars := struct{ ID string }{ID: "id1"}
				return q.NamedGetContext(ctx, &p, "SELECT * from peeps WHERE id=:id", pars)
			})
			assert.Assert(t, err)
			assert.DeepEqual(t, p, person1)
		})

		t.Run("get named db", func(t *testing.T) {
			p := person{}
			pars := struct{ ID string }{ID: "id1"}
			err := fix.TX.NoTx().NamedGetContext(ctx, &p, "SELECT * from peeps WHERE id=:id", pars)
			assert.Assert(t, err)
			assert.DeepEqual(t, p, person1)
		})
	})
}
