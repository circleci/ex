package http

import (
	"context"
	"net/http"
	"testing"

	"github.com/circleci/go-o11y"

	"gotest.tools/v3/assert"
)

func TestGetBaggage(t *testing.T) {
	ctx := context.Background()
	t.Run("no baggage", func(t *testing.T) {
		req := &http.Request{}
		assert.DeepEqual(t, getBaggage(ctx, req), o11y.Baggage{})
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
		assert.DeepEqual(t, getBaggage(ctx, req), expected)
	})
}
