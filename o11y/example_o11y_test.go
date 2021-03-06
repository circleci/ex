package o11y_test

import (
	"context"
	"fmt"
	"net"

	"github.com/circleci/ex/o11y"
)

type Worker struct{}

// Work must use named returns for the defer to capture the return value correctly.
func (w *Worker) Work(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "job-store: job-info")
	defer o11y.End(span, &err) // the pointer is needed to grab the changing content of the returned error.
	span.AddField("add-other", "fields as needed")
	o11y.AddField(ctx, "also-via", "context")

	// Do some work, using the modified context
	if _, err := (&net.Dialer{}).DialContext(ctx, "tcp", "localhost:80"); err != nil {
		return fmt.Errorf("i am the error the span.End call will use: %w", err)
	}

	return nil
}

// ExampleEndDefer shows the correct way to capture the error named return.
func Example_endDefer() {
	ctx := context.Background()
	w := Worker{}
	_ = w.Work(ctx)
}
