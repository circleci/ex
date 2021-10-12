package api

import (
	"net/http"
	"testing"

	"github.com/circleci/ex/testing/testcontext"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestHelloWorld(t *testing.T) {
	ctx := testcontext.Background()
	api := startAPI(ctx, t)

	m := make(map[string]interface{})
	status := get(t, api.url+"/api/hello", &m)
	assert.Check(t, cmp.Equal(status, http.StatusOK))
	assert.Check(t, cmp.DeepEqual(m, map[string]interface{}{"hello": "world!"}))
}
