package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/semconv"
)

// Error wraps the grpc error to decide if the error is an o11y warning.
type Error struct {
	server bool
	err    error
}

func (e *Error) Error() string {
	return e.err.Error()
}

func (e *Error) Unwrap() error {
	return e.err
}

// Type is checked by the otel package to set the error.type attribute. It should be low cardinality.
func (e *Error) Type() string {
	// The client side errors may be known errors with default types.
	de := semconv.DefaultErrorType(e.err)
	if de != "" {
		return de
	}
	// Since we intercept both client context errors above, the server context errors are returned as such.
	//nolint:exhaustive // there's a default case
	switch status.Code(e.err) {
	case codes.Canceled:
		return "server_canceled"
	case codes.DeadlineExceeded:
		return "server_deadline_exceeded"
	default:
		// Otherwise returns the string of the status code.
		return status.Code(e.err).String()
	}
}

// Retryable returns true if the error was caused by a problem that could succeed if resubmitted unchanged.
func (e *Error) Retryable() bool {
	// Client side context errors can be retried.
	if errors.Is(e.err, context.Canceled) || errors.Is(e.err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(e.err) {
	// n.b. unknown may or may not be safe to retry, default to safety.
	case codes.OK, // just for completeness
		codes.Unknown,
		codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Unimplemented,
		codes.DataLoss,
		codes.Unauthenticated:
		return false
	case codes.DeadlineExceeded,
		codes.Canceled,
		codes.ResourceExhausted,
		codes.Unavailable,
		codes.Aborted,
		codes.Internal:
		return true
	default:
		return false // Safer to not retry for new codes until we add them
	}
}

// Retryable returns true if the error carries a gRPC status code that indicates the call my succeed, if attempted
// again. Specific servers may have different requirements for what should be retried, so double check that before
// using this.
func Retryable(err error) bool {
	ge := &Error{}
	if errors.As(err, &ge) {
		return ge.Retryable()
	}
	return false
}

// Is checks that this error is being checked for the special o11y error that is not
// added to the trace as an error. If the error is due to relatively expected failure response codes
// return true so it will appear in the trace as a warning.
// N.B. We should not see 3XX codes normally in our client responses, so we currently consider them errors.
func (e *Error) Is(target error) bool {
	switch {
	case !o11y.IsWarningNoUnwrap(target):
		return false
	case errors.Is(e.err, context.Canceled):
		return true
	case e.server:
		switch status.Code(e.err) {
		case codes.Unknown,
			codes.DeadlineExceeded,
			codes.Unimplemented,
			codes.Internal,
			codes.Unavailable,
			codes.DataLoss:
			return false
		case codes.OK,
			codes.Canceled,
			codes.InvalidArgument,
			codes.NotFound,
			codes.AlreadyExists,
			codes.PermissionDenied,
			codes.ResourceExhausted,
			codes.FailedPrecondition,
			codes.Aborted,
			codes.OutOfRange,
			codes.Unauthenticated:
			return true
		}
	default:
		switch status.Code(e.err) {
		case codes.Unknown,
			codes.DeadlineExceeded,
			codes.Unimplemented,
			codes.Internal,
			codes.Unavailable,
			codes.DataLoss,
			codes.FailedPrecondition,
			codes.OutOfRange,
			codes.Unauthenticated:
			return false
		case codes.OK,
			codes.Canceled,
			codes.NotFound,
			codes.PermissionDenied,
			codes.InvalidArgument,
			codes.AlreadyExists,
			codes.ResourceExhausted,
			codes.Aborted:
			return true
		}
	}
	return false
}
