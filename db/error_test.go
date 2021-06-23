package db

import (
	"fmt"
	"testing"

	"github.com/lib/pq"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

func TestMapError(t *testing.T) {
	err := &pq.Error{
		Code: "57014",
	}
	ok, e := mapError(err)
	assert.Assert(t, ok)
	assert.Assert(t, o11y.IsWarning(e))
	e = fmt.Errorf("foo: %w", e)
	assert.Assert(t, o11y.IsWarning(e))
}
