package db

import (
	"database/sql"
	"database/sql/driver"
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
	ErrBadConn     = o11y.NewWarning("bad connection")
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

// mapError maps a few pq errors to errors defined in this package, some wrapping the original
// error. If a mapping was made the returned bool will be true, if not the original error is returned and
// the bool will be false.
func mapError(err error) (bool, error) {
	if ok, e := mapBadCon(err); ok {
		return true, e
	}
	e := &pq.Error{}
	if errors.As(err, &e) {
		switch e.Code {
		case pgForeignKeyConstraintErrorCode:
			return true, fmt.Errorf("%w: %s - %s", ErrConstrained, e.Message, e.Detail)
		case pgExceptionRaised:
			return true, fmt.Errorf("%w: %s - %s", ErrException, e.Message, e.Detail)
		case pgStatementCanceled:
			return true, fmt.Errorf("%w: %s - %s", ErrCanceled, e.Message, e.Detail)
		case pgUniqueViolationErrorCode:
			return true, fmt.Errorf("%w: %s - %s", ErrNop, e.Message, e.Detail)
		}
	}
	return false, err
}

func mapBadCon(err error) (bool, error) {
	if errors.Is(err, driver.ErrBadConn) {
		return true, ErrBadConn
	}
	return false, err
}

func badConn(err error) bool {
	return errors.Is(err, ErrBadConn)
}
