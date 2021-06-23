package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/jmoiron/sqlx"
)

// unifiedTx wraps the Querier subset of methods of the standard sqlx.DB with helpers to return our standard
// errors.
type unifiedTx struct {
	tx *sqlx.Tx
}

func (u unifiedTx) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := u.tx.ExecContext(ctx, query, args...)
	return result, mapExecErrors(err, result)
}

func (u unifiedTx) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	err := u.tx.GetContext(ctx, dest, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNop
	}
	_, err = mapError(err)
	return err
}

func (u unifiedTx) NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error) {
	result, err := u.tx.NamedExecContext(ctx, query, arg)
	return result, mapExecErrors(err, result)
}

func (u unifiedTx) SelectContext(ctx context.Context,
	dest interface{}, query string, args ...interface{}) error {

	if err := u.tx.SelectContext(ctx, dest, query, args...); err != nil {
		_, err = mapError(err)
		return err // This error never represents the no rows condition
	}
	// SelectContext has asserted dest is a pointer to a slice
	value := reflect.ValueOf(dest)
	direct := reflect.Indirect(value)
	if direct.Len() == 0 {
		return ErrNop
	}
	return nil
}
