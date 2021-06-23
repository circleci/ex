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
	assert.Check(t, ok)
	assert.Check(t, o11y.IsWarning(e))
	assert.Check(t, o11y.IsWarning(fmt.Errorf("foo: %w", e)))
}
