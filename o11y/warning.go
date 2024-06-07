package o11y

import (
	"context"
	"errors"
)

// NewWarning will return a generic error that can be tested for warning.
// No two errors created with NewWarning will be tested as equal with Is.
func NewWarning(warn string) error {
	return &wrapWarnError{
		msg: warn,
		err: errWarning,
	}
}

// sentinel warning to use with errors.Is in IsWarning
var errWarning = errors.New("")

// IsWarning returns true if any error in the chain is a warning, or if any joined errors are a warning.
func IsWarning(err error) bool {
	return errors.Is(err, errWarning)
}

// IsWarningNoUnwrap returns true if err itself is a warning.
// This will not check wrapped errors. This can be used in Is in other errors
// to check if it is being directly tested for warning.
func IsWarningNoUnwrap(err error) bool {
	// nolint: errorlint // This is intentionally not unwrapping, because of how it is expected to be used
	return err == errWarning
}

// DontErrorTrace returns true if all errors in the chain is a warning or context canceled or context deadline errors.
func DontErrorTrace(err error) bool {
	return IsWarning(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// wrapWarnError is a wrapping error to be tested for warning.
type wrapWarnError struct {
	msg string
	err error
}

func (e *wrapWarnError) Error() string {
	return e.msg
}

func (e *wrapWarnError) Unwrap() error {
	return e.err
}

// allWarningError will only match as a warning if all the top level errors it might wrap are warning
type allWarningError struct {
	err error
}

// AllWarning returns the err wrapped such that if it has Joined errors, then each error must be a warning
// for the returned err to respond true to IsWarning.
func AllWarning(err error) error {
	return &allWarningError{
		err: err,
	}
}

func (e *allWarningError) Error() string {
	return e.err.Error()
}

// N.B. We can not unwrap because errors.Is would see any of the warnings and then test +ve for warning.

// Is will only return true when matched against the warning sentinel if all joined errors are warnings,
// or if a single warning error is wrapped.
func (e *allWarningError) Is(target error) bool {
	if IsWarningNoUnwrap(target) {
		// Check if a joined error has at least one none warning
		if uw, ok := e.err.(interface{ Unwrap() []error }); ok {
			for _, err := range uw.Unwrap() {
				if !errors.Is(err, errWarning) {
					return false
				}
			}
		}
	}
	// Since we are not unwrapping as per the note above we have to do the normal Is
	// tests (for non warning types) here
	return errors.Is(e.err, target)
}

func (e *allWarningError) As(target any) bool {
	// Since we are not unwrapping as per the note above, we have to do the normal As test here
	return errors.As(e.err, target)
}
