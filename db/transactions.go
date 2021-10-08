package db

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/circleci/ex/o11y"
)

type TxManager struct {
	db *sqlx.DB
	// This is only for testing purposes
	testQuerier func(Querier) Querier
}

func NewTxManager(db *sqlx.DB) *TxManager {
	return &TxManager{
		db: db,
	}
}

type queryFn func(context.Context, Querier) error

// WithTx wraps f in an explicit o11y'd transaction, handling rollback
// if f returns an error. It will retry the transaction a few times in the face of
// ErrBadConn errors.
// The length here is due to the internalised func, which we want to encapsulate
// to avoid reuse, since it is highly coupled to the retry behaviour.
//nolint:funlen
func (t *TxManager) WithTx(ctx context.Context, f queryFn) (err error) {
	// Set up the main transaction function that we will retry on ErrBadCon
	transaction := func(attempt int) (err error) {
		ctx, span := o11y.StartSpan(ctx, "tx-manager: tx")
		defer o11y.End(span, &err)

		span.AddField("attempt", attempt)

		tx, err := t.db.BeginTxx(ctx, nil)
		if err != nil {
			_, err = mapBadCon(err)
			return fmt.Errorf("begin transaction: %w", err)
		}

		// This defer is to catch any error from the call to f to decide if we should commit
		// or rollback the transaction.
		// The defer stacking rules mean that this will be called before the bad connection
		// defer above.
		defer func() {
			p := recover()
			switch {
			case p != nil:
				// a panic occurred, attempt a rollback and re-panic
				// (it may already be rolled-back, so ignore this error)
				_ = tx.Rollback()
				panic(p)
			case badConn(err):
				// We can't do anything else with a bad connection, and the db server
				// will already have rolled back
				return
			case err != nil:
				// Never commit on an error.

				// But don't roll back if the transaction context is Done
				// (the library code already handles rollback in the context Done cases)
				// This check is in case f returned an err different from the context error
				if ctx.Err() != nil {
					return
				}
				// something other than a context cancel went wrong, rollback and report any
				// error on rollback, an ErrBadCon is safe here since the server will have rolled back
				if rErr := tx.Rollback(); rErr != nil {
					o11y.AddField(ctx, "rollback_error", rErr)
				}
			case ctx.Err() != nil:
				// This case is if f suppressed an error but the transaction ctx is still Done
				// even if f appeared to have not seen any error we report the context cancellation
				// so the calling code will at least be able to be aware that the transaction was
				// rolled back
				err = ctx.Err()
			default:
				// All good, commit
				err = tx.Commit()
				// Specifically trap the bad connection here which will allow a retry
				_, err = mapBadCon(err)
				if err != nil {
					err = fmt.Errorf("commit transaction: %w", err)
				}
				// N.B there is no need for an explicit rollback - the db server automatically rolls back
				// transactions where the connection (or session) is dropped (ErrBadConn).
			}
		}()

		// Use the error wrapped transaction so that the common errors can be reported as warnings in any
		// spans used in f
		var q Querier = unifiedQuerier{q: tx}
		if t.testQuerier != nil {
			q = t.testQuerier(tx)
		}
		err = f(ctx, q) // This err will be mapped in the unifiedQuerier wrapper

		// Note that the above defer can reassign err
		return err
	}

	// Attempt the transaction a few times.
	// (More than 3 ErrBadCon errors is going to be very unlikely.)
	for i := 0; i < 3; i++ {
		err = transaction(i)
		// We can only retry on bad connection errors
		if !badConn(err) {
			break
		}
	}
	return err
}

func (t *TxManager) NoTx() Querier {
	return unifiedQuerier{q: t.db}
}

// WithTransaction simply delegates to WithTx.
// Deprecated
func (t *TxManager) WithTransaction(ctx context.Context, f queryFn) (err error) {
	return t.WithTx(ctx, f)
}
