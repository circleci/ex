package baggage

import (
	"context"
	"net/http"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/o11y"
)

func TestGet(t *testing.T) {
	ctx := context.Background()
	t.Run("no baggage", func(t *testing.T) {
		req := &http.Request{}
		assert.Check(t, cmp.DeepEqual(Get(ctx, req), o11y.Baggage{}))
	})

	t.Run("build url", func(t *testing.T) {
		h := http.Header{}
		h.Set("otcorrelations", "build-url=https%3A%2F%2Fcircleci.com%2Fgh%2Fcircleci%2Fdistributor%2F123,foo=bar")
		req := &http.Request{
			Header: h,
		}
		expected := o11y.Baggage{
			"build-url": "https://circleci.com/gh/circleci/distributor/123",
			"foo":       "bar",
		}
		assert.Check(t, cmp.DeepEqual(Get(ctx, req), expected))
	})
}
