package dbfixture

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/jmoiron/sqlx"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/db"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/internal/types"
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

	// Inject the new user setup statement into the schema generation
	const createRwRoleTpl = `
DO $$
	BEGIN
		IF NOT EXISTS (SELECT * FROM pg_user WHERE usename = '{{ .DBName }}_app_rw') THEN
			CREATE ROLE {{ .DBName }}_app_rw password '{{ .Password }}';
			GRANT CONNECT ON DATABASE {{ .DBName }} TO {{ .DBName }}_app_rw;
			ALTER ROLE {{ .DBName }}_app_rw WITH LOGIN;
		END IF;
	END
$$;    
`
	buf := &bytes.Buffer{}
	tpl := template.Must(template.New("rw_role").Parse(createRwRoleTpl))
	err := tpl.Execute(buf, struct {
		DBName   string
		Password string
	}{
		DBName:   con.User,
		Password: con.Password.Value(),
	})
	assert.Assert(t, err)
	schema = schema + buf.String()

	println(">>>>>>>>>>>>", schema)

	db, err = shared.Manager().NewDB(ctx, con, t.Name(), schema)
	assert.Assert(t, err)
	t.Cleanup(func() {
		p := o11y.FromContext(ctx)
		ctx, cancel := context.WithTimeout(o11y.WithProvider(context.Background(), p), 10*time.Second)
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

func (m *Manager) newDB(ctx context.Context,
	d *sqlx.DB, con Connection, dbName, schema string) (_ *Fixture, err error) {

	ctx, span := o11y.StartSpan(ctx, "dbfixture: newDB")
	defer o11y.End(span, &err)

	fix := &Fixture{DBName: dbName, Host: con.Host, User: con.User, Password: con.Password}
	span.AddField("dbname", fix.DBName)
	span.AddField("host", con.Host)
	span.AddField("user", con.User)
	createDB := fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{fix.DBName}.Sanitize())
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

	db, err = sqlx.Open("pgx", uri.String())
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

	// attempt to kick out any melingering connections before dropping the database
	_, err = db.ExecContext(ctx,
		fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM public;", pgx.Identifier{fixture.DBName}.Sanitize()))
	if err != nil {
		return fmt.Errorf("revoke con: %w", err)
	}

	killConSQL := `
SELECT pid, pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`
	_, err = db.ExecContext(ctx, fmt.Sprintf(killConSQL, fixture.DBName))
	if err != nil {
		o11y.LogError(ctx, "db: cleanup drop con", err)
	}

	dbName := pgx.Identifier{fixture.DBName}.Sanitize()
	_, err = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
	if err != nil {
		return fmt.Errorf("drop db: %w", err)
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
	DBName   string
	Host     string
	User     string
	Password secret.String

	// If AddAppUser is called these will be related to the app user, and the Admin field will be set to the
	// original DB Admin.
	DB *sqlx.DB
	TX *db.TxManager

	Admin   *sqlx.DB
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
			_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s`,
				pgx.Identifier{table.Schema, table.Name}.Sanitize()))
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

func (f *Fixture) AddAppUser(ctx context.Context, t types.TestingTB, user string) string {
	t.Helper()

	// Inject the new user setup statement into the schema generation
	const createRwRoleTpl = `
DO $$
	BEGIN
		IF NOT EXISTS (SELECT * FROM pg_user WHERE usename = '{{ .UserName }}') THEN
			CREATE ROLE {{ .UserName }} password '{{ .Password }}';
			GRANT CONNECT ON DATABASE {{ .DBName }} TO {{ .UserName }};
			ALTER ROLE {{ .UserName }} WITH LOGIN;
		END IF;
	END
$$;    
`
	buf := &bytes.Buffer{}
	tpl := template.Must(template.New("rw_role").Parse(createRwRoleTpl))
	err := tpl.Execute(buf, struct {
		DBName   string
		UserName string
		Password string
	}{
		DBName:   f.User,
		UserName: user,
		Password: f.Password.Value(),
	})
	assert.Assert(t, err)

	println(">>>>>>>>>>>>", buf.String())

	o11y.Log(ctx, "applying schema")
	_, err = f.DB.ExecContext(ctx, buf.String())
	assert.Assert(t, err)

	return buf.String()
}

func squelchNopError(err error) error {
	if err != nil && !errors.Is(err, db.ErrNop) {
		return err
	}
	return nil
}
