package db

import (
	"context"
	"database/sql/driver"
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

	// Retry this transaction 3 times
	for i := 0; i < 3; i++ {
		err = s.WithOneTransaction(ctx, f)
		if !errors.Is(err, ErrBadConn) {
			break
		}
		o11y.AddField(ctx, "bad_con", i)
		o11y.AddField(ctx, "warning", err)
	}

	// Note that the above defer can reassign err
	return err
}

func (s *TxManager) WithOneTransaction(ctx context.Context, f func(context.Context, Querier) error) (err error) {
	ctx, span := o11y.StartSpan(ctx, "tx-manager: with-one-transaction")
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
			// but don't roll back if the transaction context is Done
			// (the library code already handles rollback in the context Done cases)
			if ctx.Err() != nil {
				return
			}
			// something other than a context cancel went wrong, rollback and report any
			// error on rollback
			if rErr := tx.Rollback(); rErr != nil {
				o11y.AddField(ctx, "rollback_error", rErr)
			}
		case ctx.Err() != nil:
			// f may have suppressed an error but the transaction ctx is still Done
			// even if f appeared to have not seen any error we report the context cancellation
			// so the client will at least be able to be aware that the transaction was rolled back
			err = ctx.Err()
		default:
			// all good, commit
			err = tx.Commit()
			// specifically trap the bad connection here which will allow a retry
			if errors.Is(err, driver.ErrBadConn) {
				err = ErrBadConn
			}
			// N.B there is no need for an explicit rollback - the db server automatically rolls back
			// transactions where the connection (or session) is dropped.
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
