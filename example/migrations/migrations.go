package migrations

import (
	"context"
	_ "embed"
	"testing"

	"github.com/circleci/ex/testing/dbfixture"
)

func SetupDB(ctx context.Context, t testing.TB) *dbfixture.Fixture {
	return dbfixture.SetupDB(ctx, t, schema, dbfixture.Connection{
		Host:     "localhost:5432",
		User:     "user",
		Password: "password",
	})
}

var (
	//go:embed schema.sql
	schema string
)
