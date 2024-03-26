package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
)

func TestMapError(t *testing.T) {
	const (
		message = "pq message"
		detail  = "pq detail"
	)
	for _, fix := range []struct {
		errCode       string
		expectWarning bool
		expectErrMsg  string
		expectError   error
	}{
		{
			errCode:      pgForeignKeyConstraintErrorCode,
			expectErrMsg: "violates constraints: pq message - pq detail",
			expectError:  ErrConstrained,
		},
		{
			errCode:       pgUniqueViolationErrorCode,
			expectErrMsg:  "no update or results: pq message - pq detail",
			expectWarning: true,
			expectError:   ErrNop,
		},
		{
			errCode:      pgExceptionRaised,
			expectErrMsg: "exception: pq message - pq detail",
			expectError:  ErrException,
		},
		{
			errCode:       pgStatementCanceled,
			expectErrMsg:  "statement canceled: pq message - pq detail",
			expectWarning: true,
			expectError:   ErrCanceled,
		},
		{
			errCode:       "a-non-sentinel-error",
			expectErrMsg:  "FATAL: pq message (SQLSTATE a-non-sentinel-error)",
			expectWarning: false,
			expectError:   nil,
		},
	} {
		t.Run("code-"+fix.errCode, func(t *testing.T) {
			pqErr := &pgconn.PgError{
				Code:     fix.errCode,
				Severity: "FATAL",
				Message:  message,
				Detail:   detail,
			}

			testErr := func(prefix string, e error) {
				assert.Check(t, cmp.Equal(e.Error(), prefix+fix.expectErrMsg))
				if fix.expectWarning {
					assert.Check(t, o11y.IsWarning(e))
					assert.Check(t, o11y.IsWarning(fmt.Errorf("foo: %w", e)))
				} else {
					assert.Check(t, !o11y.IsWarning(e))
				}

				pqErr := PqError(e)
				if pqErr == nil {
					t.Error("unexpected nil pq.Error")
					return
				}
				if fix.expectError != nil {
					assert.Check(t, errors.Is(e, fix.expectError))
				}
				assert.Check(t, cmp.Equal(fix.errCode, pqErr.Code))

				dbe := &Error{}
				assert.Check(t, errors.As(e, &dbe) && dbe.PqError().Code == fix.errCode)
			}

			t.Run("direct", func(t *testing.T) {
				ok, mappedErr := mapError(pqErr)
				assert.Check(t, ok)
				testErr("", mappedErr)
			})

			//t.Run("wrapped", func(t *testing.T) {
			//	ok, mappedErr := mapError(pqErr)
			//	assert.Check(t, ok)
			//	testErr("wrapped: ", fmt.Errorf("wrapped: %w", mappedErr))
			//})
		})
	}
}

func TestError_Is(t *testing.T) {
	for _, e := range []error{
		ErrNop,
		ErrConstrained,
		ErrException,
		ErrCanceled,
		ErrBadConn,
	} {
		t.Run(e.Error(), func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", &Error{
				pqErr:    nil,
				sentinel: e,
			})
			assert.Check(t, errors.Is(err, e))
		})
	}
}
