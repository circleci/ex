package testrand

import (
	"encoding/hex"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestRandHex(t *testing.T) {
	for i := 1; i < 128; i++ {
		h := Hex(i)
		assert.Check(t, cmp.Equal(len(h), i))
		if i%2 == 0 {
			b, err := hex.DecodeString(h)
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(len(b), i/2), b)
		} else {
			_, err := hex.DecodeString(h)
			assert.ErrorContains(t, err, "odd length hex string")
		}
	}
}
