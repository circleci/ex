package o11y

import "errors"

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

// IsWarning returns true if any error in the chain is a warning.
func IsWarning(err error) bool {
	return errors.Is(err, errWarning)
}

// IsWarningNoUnwrap returns true if err itself is a warning.
// This will not check wrapped errors. This can be used in Is in other errors
// to check if it is being directly tested for warning.
func IsWarningNoUnwrap(err error) bool {
	return err == errWarning
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
