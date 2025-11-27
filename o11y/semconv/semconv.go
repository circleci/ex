package semconv

import (
	"context"
	"errors"
	"io"
)

// DefaultErrorType will return the low cardinality error type for any recognised err.
func DefaultErrorType(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, io.EOF):
		return "eof"
	default:
		return ""
	}
}
