package o11y_test

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
)

func TestPropagation(t *testing.T) {
	p, err := otel.New(otel.Config{})
	assert.NilError(t, err)

	var prop string
	func() {
		ctx := o11y.WithProvider(context.Background(), p)
		ctx, span := o11y.StartSpan(ctx, "foo")
		defer span.End()
		ctx = o11y.WithBaggage(ctx, o11y.Baggage{
			"bg1": "bgv1",
			"bg2": "bgv2",
		})
		prop = o11y.Propagation(ctx)
	}()

	ctx := o11y.WithProvider(context.Background(), p)
	ctx, span := o11y.SpanFromPropagation(ctx, "new", prop)
	bag := o11y.GetBaggage(ctx)
	span.End()
	p.Close(ctx)

	assert.Check(t, cmp.Equal(bag["bg1"], "bgv1"))
	assert.Check(t, cmp.Equal(bag["bg2"], "bgv2"))
}
