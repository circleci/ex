package db

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/circleci/ex/o11y"
)

var (
	ErrNop         = o11y.NewWarning("no update or results")
	ErrConstrained = errors.New("violates constraints")
	ErrException   = errors.New("exception")
	ErrCanceled    = o11y.NewWarning("statement canceled")
)

const (
	pgForeignKeyConstraintErrorCode = "23503"
	pgUniqueViolationErrorCode      = "23505"
	pgExceptionRaised               = "P0001"
	pgStatementCanceled             = "57014"
)

func mapExecErrors(err error, res sql.Result) error {
	found, err := mapError(err)
	if found {
		return err
	}
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNop
	}
	return nil
}

// mapError maps a few pq errors to a errors defined in this package, some wrapping the original
// error. If a mapping was made the returned bool will be true, if not the original error is returned and
// the bool will be false.
// nolint: golint - error, bool is fine for error mapping funcs
func mapError(err error) (bool, error) {
	e := &pq.Error{}
	if errors.As(err, &e) {
		switch e.Code {
		case pgForeignKeyConstraintErrorCode:
			return true, ErrConstrained
		case pgExceptionRaised:
			return true, fmt.Errorf("%w: %s", ErrException, e.Message)
		case pgStatementCanceled:
			return true, fmt.Errorf("%w: %s", ErrCanceled, e.Message)
		case pgUniqueViolationErrorCode:
			return true, fmt.Errorf("%w: %s", ErrNop, e.Message)
		}
	}
	return false, err
}
