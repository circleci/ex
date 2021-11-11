package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
)

// unifiedQuerier wraps the Querier subset of methods of the standard sqlx.DB with helpers to return our standard
// errors.
type unifiedQuerier struct {
	q Querier
}

func (u unifiedQuerier) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := u.q.ExecContext(ctx, query, args...)
	return result, mapExecErrors(err, result)
}

func (u unifiedQuerier) GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	err := u.q.GetContext(ctx, dest, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNop
	}
	_, err = mapError(err)
	return err
}

func (u unifiedQuerier) NamedGetContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	err := u.q.NamedGetContext(ctx, dest, query, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNop
	}
	_, err = mapError(err)
	return err
}

func (u unifiedQuerier) NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error) {
	result, err := u.q.NamedExecContext(ctx, query, arg)
	return result, mapExecErrors(err, result)
}

func (u unifiedQuerier) SelectContext(ctx context.Context,
	dest interface{}, query string, args ...interface{}) error {

	if err := u.q.SelectContext(ctx, dest, query, args...); err != nil {
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
