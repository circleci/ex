package integration

import (
	"context"
	"errors"
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

	const schema = `
    CREATE TABLE peeps (id text PRIMARY KEY, name text, height smallint, dob timestamp);
    CREATE TABLE birbs (id text PRIMARY KEY, peep_id text, name text);
	ALTER TABLE ONLY birbs
	ADD CONSTRAINT birbs_fk FOREIGN KEY (peep_id) REFERENCES peeps(id);`
	fix := dbfixture.SetupDB(ctx, t, schema, conn)

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

		t.Run("pk violation", func(t *testing.T) {
			const sql = "INSERT INTO peeps (id,name,height,dob) VALUES (:id,:name,:height,:dob);"
			_, err := fix.TX.NoTx().NamedExecContext(ctx, sql, person1)
			assert.Check(t, errors.Is(err, db.ErrNop))
			assert.ErrorContains(t, err, "peeps_pkey")
		})

		type birb struct {
			ID     string
			Name   string
			PeepID string
		}
		// add a person
		birb1 := birb{
			ID:     "id1",
			Name:   "bob",
			PeepID: "not-exist",
		}
		t.Run("fk violation", func(t *testing.T) {
			const sql = "INSERT INTO birbs (id,name, peep_id) VALUES (:id,:name,:peepid);"
			_, err := fix.TX.NoTx().NamedExecContext(ctx, sql, birb1)
			assert.Check(t, errors.Is(err, db.ErrConstrained))
			assert.ErrorContains(t, err, "birbs_fk")

			// an example of how to extract the constraint that failed
			if errors.Is(err, db.ErrConstrained) && db.PqError(err).Constraint == "birbs_fk" {
				// do your constraint behaviour
			} else {
				t.Error("got the wrong constraint", db.PqError(err).Constraint)
			}
			// or if you want to go direct
			if db.PqError(err) != nil && db.PqError(err).Constraint == "birbs_fk" {
				// you may need to map the code here - which would be bad.
				// but you could check the db mapping
				assert.Check(t, errors.Is(err, db.ErrConstrained))
			}
		})
	})
}
