package otel

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestAttr(t *testing.T) {
	var ip *int

	for _, tt := range []struct {
		name   string
		val    any
		expect attribute.KeyValue
	}{
		{
			name:   "string",
			val:    "hello",
			expect: attribute.String("string", "hello"),
		},
		{
			name:   "bool",
			val:    true,
			expect: attribute.Bool("bool", true),
		},
		{
			name:   "int",
			val:    42,
			expect: attribute.Int("int", 42),
		},
		{
			name:   "int8",
			val:    int8(8),
			expect: attribute.Int("int8", 8),
		},
		{
			name:   "int16",
			val:    int16(16),
			expect: attribute.Int("int16", 16),
		},
		{
			name:   "int32",
			val:    int32(32),
			expect: attribute.Int("int32", 32),
		},
		{
			name:   "int64",
			val:    int64(64),
			expect: attribute.Int("int64", 64),
		},
		{
			name:   "intp",
			val:    ip,
			expect: attribute.String("intp", ""),
		},
		{
			name:   "error",
			val:    errors.New("an error"),
			expect: attribute.String("error", "an error"),
		},
		{
			name:   "struct",
			val:    struct{ Data string }{Data: "some data"},
			expect: attribute.String("struct", "{some data}"),
		},
		{
			name:   "just_nil",
			val:    nil,
			expect: attribute.String("just_nil", ""),
		},
		{
			name:   "duration",
			val:    time.Millisecond * 1234,
			expect: attribute.Int("duration", 1234),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := attr(tt.name, tt.val)
			assert.DeepEqual(t, got, tt.expect, cmpopts.EquateComparable(attribute.Value{}))
		})
	}

	t.Run("float32", func(t *testing.T) {
		got := attr("float32", float32(32.32))
		assert.Check(t, cmp.DeepEqual(got.Value.AsFloat64(), 32.32, cmpopts.EquateApprox(0, 0.0001)))
	})

	t.Run("float64", func(t *testing.T) {
		got := attr("float64", float64(64.64))
		assert.Check(t, cmp.DeepEqual(got.Value.AsFloat64(), 64.64, cmpopts.EquateApprox(0, 0.0001)))
	})
}
