package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/config/secret"
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
    CREATE TABLE peeps (id text PRIMARY KEY, name text, height smallint, dob timestamp, password text, raw json);
    CREATE TABLE birbs (id text PRIMARY KEY, peep_id text, name text);
	ALTER TABLE ONLY birbs
	ADD CONSTRAINT birbs_fk FOREIGN KEY (peep_id) REFERENCES peeps(id);`
	fix := dbfixture.SetupDB(ctx, t, schema, conn)

	t.Run("statements", func(t *testing.T) {
		type person struct {
			ID       string
			Name     string
			Height   int
			DOB      time.Time
			Password secret.String
			Raw      json.RawMessage
		}
		// add a person
		person1 := person{
			ID:       "id1",
			Name:     "bob",
			Height:   187,
			DOB:      time.Date(1998, 7, 4, 0, 0, 0, 0, time.UTC),
			Password: "correct horse battery staple",
			Raw:      json.RawMessage(`{"help": "me"}`),
		}
		err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {

			const sql = `
INSERT INTO peeps 
(id,name,height,dob,password,raw) 
VALUES (:id,:name,:height,:dob,:password,:raw);
`
			_, err := q.NamedExecContext(ctx, sql, person1)

			return err
		})
		assert.Assert(t, err)

		person1.Raw = json.RawMessage(`{"help": "me"}`)

		t.Run("get", func(t *testing.T) {
			p := person{}
			err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {
				return q.GetContext(ctx, &p, "SELECT * from peeps WHERE id=$1", "id1")
			})
			assert.Assert(t, err)
			assert.DeepEqual(t, p, person1)

			var ps []person
			q := "SELECT * from peeps WHERE id=any($1)"
			err = fix.TX.NoTx().SelectContext(ctx, &ps, q, []string{"id1", "id2"})
			assert.Assert(t, err)
			assert.DeepEqual(t, p, ps[0])
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

		t.Run("insert named returning", func(t *testing.T) {
			person3 := person{
				ID:       "id3",
				Name:     "ben",
				Height:   185,
				DOB:      time.Date(1998, 7, 5, 0, 0, 0, 0, time.UTC),
				Password: "correct horse battery staple",
				Raw:      json.RawMessage(`{"help": "me"}`), // note the space
			}

			p := person{}
			const sql = `
INSERT INTO peeps 
(id,name,height,dob,password,raw) 
VALUES (:id,:name,:height,:dob,:password,:raw)
RETURNING id,name;
`
			err := fix.TX.WithTx(ctx, func(ctx context.Context, q db.Querier) error {
				return q.NamedGetContext(ctx, &p, sql, person3)
			})
			assert.Assert(t, err)
			assert.DeepEqual(t, p, person{
				ID:   "id3",
				Name: "ben",
			})
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
			assert.Check(t, cmp.ErrorContains(err, "peeps_pkey"))
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
			assert.Check(t, cmp.ErrorContains(err, "birbs_fk"))

			//an example of how to extract the constraint that failed
			if errors.Is(err, db.ErrConstrained) && db.PqError(err).ConstraintName == "birbs_fk" {
				// do your constraint behaviour
			} else {
				t.Error("got the wrong constraint", db.PqError(err).ConstraintName)
			}
			// or if you want to go direct
			if db.PqError(err) != nil && db.PqError(err).ConstraintName == "birbs_fk" {
				// you may need to map the code here - which would be bad.
				// but you could check the db mapping
				assert.Check(t, errors.Is(err, db.ErrConstrained))
			}
		})
	})
}
