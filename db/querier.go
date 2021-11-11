package db

import (
	"context"
	"database/sql"
)

// Querier can either be a *sqlx.DB or *sqlx.Tx
type Querier interface {
	// The following are implemented by sql.DB, sql.Tx

	// ExecContext executes the query with placeholder parameters that match the args.
	// Use this if the query does not use named parameters (for that use NamedExecContext),
	// and you do not care about the data the query generates.
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// The following are implemented by sqlx.DB, sqlx.Tx

	// GetContext expects placeholder parameters in the query and will bind args to them.
	// A single row result will be mapped to dest which must be a pointer to a struct.
	// In the case of no result the error returned will be sql.ErrNoRows.
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error

	// NamedGetContext expect a query with named parameters, fields from the arg struct will be mapped
	// to the named parameters. A single row result will be mapped to dest which must be a pointer to a struct.
	NamedGetContext(ctx context.Context, dest interface{}, query string, arg interface{}) error

	// NamedExecContext expect a query with named parameters, fields from the arg struct will be mapped
	// to the named parameters. Use this if you do not care about the data the query generates, and
	// you don't want to use placeholder parameters (see ExecContext)
	NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)

	// SelectContext expects placeholder parameters in the query and will bind args to them.
	// Each resultant row will be scanned into dest, which must be a slice.
	// (If you expect (or want) a single row in the response use GetContext instead.)
	// This method never returns sql.ErrNoRows, instead the dest slice will be empty.
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}
