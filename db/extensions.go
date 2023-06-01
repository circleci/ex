package db

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// wrappers to implement a removed or never existing method that folk find useful
type eDB struct {
	*sqlx.DB
}

func (e eDB) NamedGetContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	namedQuery, args, err := e.DB.BindNamed(query, arg)
	if err != nil {
		return fmt.Errorf("could not map named: %w", err)
	}
	return e.GetContext(ctx, dest, namedQuery, args...)
}

func (e eDB) NamedSelectContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	namedQuery, args, err := e.DB.BindNamed(query, arg)
	if err != nil {
		return fmt.Errorf("could not map named: %w", err)
	}
	return e.SelectContext(ctx, dest, namedQuery, args...)
}

type eTx struct {
	*sqlx.Tx
}

func (e eTx) NamedGetContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	namedQuery, args, err := e.Tx.BindNamed(query, arg)
	if err != nil {
		return fmt.Errorf("could not map named: %w", err)
	}
	return e.GetContext(ctx, dest, namedQuery, args...)
}

func (e eTx) NamedSelectContext(ctx context.Context, dest interface{}, query string, arg interface{}) error {
	namedQuery, args, err := e.Tx.BindNamed(query, arg)
	if err != nil {
		return fmt.Errorf("could not map named: %w", err)
	}
	return e.SelectContext(ctx, dest, namedQuery, args...)
}
