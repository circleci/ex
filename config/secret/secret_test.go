package secret

import (
	"encoding/json"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestSecret(t *testing.T) {
	s := String("secret")
	assert.Check(t, cmp.Equal(s.Value(), "secret"))
	assert.Check(t, cmp.Equal(fmt.Sprintf("%v", s), "REDACTED"))
	assert.Check(t, cmp.Equal(s.String(), "REDACTED"))
	assert.Check(t, cmp.Equal(s.GoString(), "REDACTED"))

	// json will marshal the underlying secret
	b, err := json.Marshal(s)
	assert.Assert(t, err)
	assert.Check(t, cmp.Equal(string(b), `"REDACTED"`))
}
