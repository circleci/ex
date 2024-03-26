package db

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

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
	e := &pgconn.PgError{}
	if errors.As(err, &e) {
		switch e.Code {
		case pgForeignKeyConstraintErrorCode:
			return true, &Error{sentinel: ErrConstrained, pqErr: e}
		case pgExceptionRaised:
			return true, &Error{sentinel: ErrException, pqErr: e}
		case pgStatementCanceled:
			return true, &Error{sentinel: ErrCanceled, pqErr: e}
		case pgUniqueViolationErrorCode:
			return true, &Error{sentinel: ErrNop, pqErr: e}
		}
		return true, &Error{pqErr: e}
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

// Error wraps a pq.Error to make it available to the caller.
// The sentinel is included for easier testing of the existing error vars.
// for example errors.Is(err, ErrConstrained)
type Error struct {
	pqErr    *pgconn.PgError
	sentinel error
}

// PqError will return any wrapped pqError is e has one
func (e Error) PqError() *pgconn.PgError {
	return e.pqErr
}

// Is checks that this error is being checked for the special o11y error that is not
// added to the trace as an error. If the error is due to relatively expected failure response codes
// return true so it does not appear in the traces as an error.
func (e *Error) Is(target error) bool {
	if e == nil {
		return false
	}
	if o11y.IsWarningNoUnwrap(target) {
		return o11y.IsWarning(e.sentinel)
	}
	return errors.Is(target, e.sentinel)
}

// Error returns the standard sentinel error format and then the underlying pq.Error
// if one exists
func (e *Error) Error() string {
	if e.sentinel != nil {
		if e.pqErr != nil {
			return fmt.Sprintf("%s: %s - %s", e.sentinel.Error(), e.pqErr.Message, e.pqErr.Detail)
		}
		return e.sentinel.Error()
	}
	if e.pqErr != nil {
		return e.pqErr.Error()
	}
	return "unknown database error"
}

func PqError(err error) *pgconn.PgError {
	e := &Error{}
	if errors.As(err, &e) {
		return e.pqErr
	}
	return nil
}
