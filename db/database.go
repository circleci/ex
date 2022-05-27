package db

import (
	"context"
	"fmt"
	"net/url"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/jmoiron/sqlx"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
)

type Config struct {
	Host string
	Port int
	User string
	Pass secret.String
	Name string
	SSL  bool

	// If these are unset, then defaults will be chosen
	ConnMaxLifetime time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
}

// New connects to a database. The context passed in is expected to carry an o11y provider
// and is only used for reporting (not for cancellation),
func New(ctx context.Context, dbName, appName string, options Config) (db *sqlx.DB, err error) {
	_, span := o11y.StartSpan(ctx, "config: connect to database")
	defer o11y.End(span, &err)

	host := fmt.Sprintf("%s:%d", options.Host, options.Port)

	span.AddField("database", dbName)
	span.AddField("host", host)
	span.AddField("dbname", options.Name)
	span.AddField("username", options.User)

	params := url.Values{}
	params.Set("connect_timeout", "5")
	params.Set("application_name", appName)
	if options.SSL {
		params.Set("sslmode", "require")
	} else {
		params.Set("sslmode", "disable")
	}

	uri := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(options.User, options.Pass.Value()),
		Host:     host,
		Path:     options.Name,
		RawQuery: params.Encode(),
	}
	db, err = sqlx.Open("pgx", uri.String())
	if err != nil {
		return nil, err
	}

	if options.ConnMaxLifetime == 0 {
		db.SetConnMaxLifetime(time.Hour)
	} else {
		db.SetConnMaxLifetime(options.ConnMaxLifetime)
	}

	if options.MaxOpenConns == 0 {
		db.SetMaxOpenConns(100)
	} else {
		db.SetMaxOpenConns(options.MaxOpenConns)
	}

	if options.MaxIdleConns == 0 {
		db.SetMaxIdleConns(100)
	} else {
		db.SetMaxIdleConns(options.MaxIdleConns)
	}

	return db, nil
}
