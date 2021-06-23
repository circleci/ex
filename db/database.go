package db

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // Load PostgresSQL Driver

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
}

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
	db, err = sqlx.Open("postgres", uri.String())
	if err != nil {
		return nil, err
	}

	db.SetConnMaxLifetime(time.Hour)
	// Chosen based on production metrics (during spikes in db io lag) to minimise waiting whilst protecting
	// the db server from possibly problematic load.
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(50)
	return db, nil
}
