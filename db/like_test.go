package db

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		in, out string
	}{
		{in: "im good", out: "im good"},
		{in: "im na_ght_y", out: `im na\_ght\_y`},
		{in: "per%ent", out: `per\%ent`},
		{in: "_im%rly %b_d", out: `\_im\%rly \%b\_d`},
	}
	for _, tt := range tests {
		assert.Equal(t, EscapeLike(tt.in), tt.out)
	}
}
