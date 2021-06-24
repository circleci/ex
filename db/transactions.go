package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/circleci/ex/o11y"
)

type TxManager struct {
	DB *sqlx.DB
	// This is only for testing purposes
	TestQuerier func(Querier) Querier
}

func NewTxManager(db *sqlx.DB) *TxManager {
	return &TxManager{DB: db}
}

func (s *TxManager) WithTransaction(ctx context.Context, f func(context.Context, Querier) error) (err error) {
	ctx, span := o11y.StartSpan(ctx, "tx-manager: with-transaction")
	defer o11y.End(span, &err)

	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}

	defer func() {
		p := recover()
		switch {
		case p != nil:
			// a panic occurred, rollback and re-panic
			_ = tx.Rollback()
			panic(p)
		case err != nil:
			// never commit on an error
			// but don't rollback if the transaction context has been canceled
			// (the library code already handles rollback in the context canceled cases)
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			// something other than a context cancel went wrong, rollback
			if rErr := tx.Rollback(); rErr != nil {
				o11y.AddField(ctx, "rollback_error", rErr)
			}
		case errors.Is(ctx.Err(), context.Canceled):
			// f may have suppressed an error but the transaction has still been cancelled
			// even if f appeared to have not seen any error we report the context cancellation
			// so the client will at least be able to be aware that the transaction was rolled back
			err = ctx.Err()
			return
		default:
			// all good, commit
			err = tx.Commit()
		}
	}()

	var q Querier = unifiedTx{tx: tx}
	if s.TestQuerier != nil {
		q = s.TestQuerier(tx)
	}
	err = f(ctx, q)

	// Note that the above defer can reassign err
	return err
}
