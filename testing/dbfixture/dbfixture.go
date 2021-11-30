package dbfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/db"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/types"
)

var globalFixture = &SharedFixture{}

var mustRunAllTests = os.Getenv("CI") == "true"

type SharedFixture struct {
	once sync.Once
	m    *Manager
}

func (s *SharedFixture) Manager() *Manager {
	return s.m
}

// SetupSystem prepares the running system for use
// callers should not rely on the fact this currently uses a package global
func SetupSystem(t types.TestingTB, con Connection) *SharedFixture {
	return setupSystem(t, con)
}

// setupSystem prepares the running system for use
func setupSystem(t types.TestingTB, con Connection) *SharedFixture {
	globalFixture.once.Do(func() {
		var err error
		globalFixture.m, err = newManager(con)
		if err != nil {
			var noDBError *NoDBError
			if errors.As(err, &noDBError) && !mustRunAllTests {
				t.Skip(noDBError.Error())
			}
			t.Fatal(err.Error())
		}
	})
	if globalFixture.m == nil {
		t.Skip("global fixtures failed setup")
	}
	return globalFixture
}

type Connection struct {
	Host     string
	User     string
	Password secret.String
}

func SetupDB(ctx context.Context, t types.TestingTB, schema string, con Connection) (db *Fixture) {
	t.Helper()
	shared := SetupSystem(t, con)
	db, err := shared.Manager().NewDB(ctx, con, t.Name(), schema)
	assert.NilError(t, err)
	t.Cleanup(func() {
		p := o11y.FromContext(ctx)
		ctx, cancel := context.WithTimeout(o11y.WithProvider(context.Background(), p), 5*time.Second)
		defer cancel()

		if r := recover(); r != nil {
			_ = db.Cleanup(ctx)
			panic(r)
		}
		assert.Assert(t, db.Cleanup(ctx))
	})
	return db
}

type Manager struct {
	db *sqlx.DB
}

// NewManager returns a DB manager
func NewManager(con Connection) (*Manager, error) {
	return newManager(con)
}

func newManager(con Connection) (*Manager, error) {
	m := &Manager{}
	var err error
	m.db, err = newDB(con, "postgres")
	if err != nil {
		return nil, err
	}
	return m, nil
}

// NewDB returns a new database fixture. The database name is generated from dbName with a random suffix.
func (m *Manager) NewDB(ctx context.Context, con Connection, dbName, schema string) (*Fixture, error) {
	s := fmt.Sprintf("%s-%s", randomSuffix(), dbName)
	l := len(s)
	if l > 63 {
		l = 63
	}
	s = s[:l]
	return m.newDB(ctx, m.db, con, s, schema)
}

func (m *Manager) newDB(ctx context.Context, d *sqlx.DB, con Connection, dbName, schema string) (
	_ *Fixture, err error) {
	ctx, span := o11y.StartSpan(ctx, "dbfixture: newDB")
	defer o11y.End(span, &err)

	fix := &Fixture{DBName: dbName}
	span.AddField("dbname", fix.DBName)
	span.AddField("host", con.Host)
	span.AddField("user", con.User)
	createDB := fmt.Sprintf("CREATE DATABASE %s", pq.QuoteIdentifier(fix.DBName))
	_, err = d.ExecContext(ctx, createDB)
	if err != nil {
		return nil, err
	}

	fix.DB, err = newDB(con, fix.DBName)
	if err != nil {
		return nil, err
	}
	fix.TX = db.NewTxManager(fix.DB)

	fix.Cleanup = func(ctx context.Context) error {
		return m.cleanup(ctx, d, fix)
	}

	err = fix.DB.Ping()
	if err != nil {
		return nil, err
	}

	o11y.Log(ctx, "applying schema")
	_, err = fix.DB.ExecContext(ctx, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	err = fix.DB.SelectContext(ctx, &fix.tables, `
SELECT
    table_name,
    table_schema
FROM
    information_schema.tables
WHERE
    table_type = 'BASE TABLE'
AND
    table_schema NOT IN ('pg_catalog', 'information_schema')
`)
	if err != nil {
		return nil, fmt.Errorf("could not get list of tables: %w", err)
	}

	// pg_dump blanks 'search_path' for security reasons, we need to set it back
	// https://www.postgresql.org/message-id/ace62b19-f918-3579-3633-b9e19da8b9de%40aklaver.com
	_, err = fix.DB.ExecContext(ctx, "SELECT pg_catalog.set_config('search_path', 'public', false);")
	if err != nil {
		return nil, err
	}

	return fix, nil
}

func (m *Manager) Close() error {
	return m.db.Close()
}

type NoDBError struct {
	err error
}

func (e *NoDBError) Error() string {
	return fmt.Sprintf("no database available: %s", e.err)
}

func (e *NoDBError) Unwrap() error {
	return e.err
}

func newDB(con Connection, name string) (db *sqlx.DB, err error) {
	params := url.Values{}
	params.Set("connect_timeout", "5")
	params.Set("sslmode", "disable")

	uri := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(con.User, con.Password.Value()),
		Host:     con.Host,
		Path:     name,
		RawQuery: params.Encode(),
	}

	db, err = sqlx.Open("postgres", uri.String())
	if err != nil {
		return nil, err
	}

	db.SetConnMaxLifetime(time.Hour)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	err = db.Ping()
	if err != nil {
		return nil, &NoDBError{err: err}
	}

	return db, nil
}

func (m *Manager) cleanup(ctx context.Context, db *sqlx.DB, fixture *Fixture) error {
	err := fixture.DB.Close()
	if err != nil {
		o11y.LogError(ctx, "db: cleanup", err)
	}

	if os.Getenv("TEST_PRESERVE_DB") != "" {
		return nil
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", pq.QuoteIdentifier(fixture.DBName)))
	if err != nil {
		return err
	}

	return nil
}

func randomSuffix() string {
	bytes := make([]byte, 3)
	if _, err := rand.Read(bytes); err != nil {
		return "not-random--i-hope-thats-ok"
	}
	return hex.EncodeToString(bytes)
}

type Fixture struct {
	DBName  string
	DB      *sqlx.DB
	TX      *db.TxManager
	Cleanup func(ctx context.Context) error

	tables []table
}

type table struct {
	Schema string `db:"table_schema"`
	Name   string `db:"table_name"`
}

func (f *Fixture) Reset(ctx context.Context) (err error) {
	return f.TX.WithTx(ctx, func(ctx context.Context, tx db.Querier) error {
		_, err = tx.ExecContext(ctx, `SET session_replication_role = 'replica';`)

		if squelchNopError(err) != nil {
			return fmt.Errorf("could not disable contraint checks: %w", err)
		}

		for _, table := range f.tables {
			// nolint: gosec
			_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s.%s`,
				pq.QuoteIdentifier(table.Schema),
				pq.QuoteIdentifier(table.Name),
			))
			if squelchNopError(err) != nil {
				return fmt.Errorf("could not delete from table: %w", err)
			}
		}

		_, err = tx.ExecContext(ctx, `SET session_replication_role = 'origin';`)
		if squelchNopError(err) != nil {
			return fmt.Errorf("could not enable contraint checks: %w", err)
		}

		return nil
	})
}

func squelchNopError(err error) error {
	if err != nil && !errors.Is(err, db.ErrNop) {
		return err
	}
	return nil
}
