package httprecorder

import (
	"net/http"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestIgnoreHeaders(t *testing.T) {
	assert.Check(t, cmp.DeepEqual(
		http.Header{
			"a": []string{"a"},
			"b": []string{"b"},
			"c": []string{"c1", "c2"},
		},
		http.Header{
			"a": []string{"a"},
			"b": []string{"difference-ignored"},
			"c": []string{"c1", "c2"},
		},
		IgnoreHeaders("b"),
	))
}

func TestOnlyHeaders(t *testing.T) {
	assert.Check(t, cmp.DeepEqual(
		http.Header{
			"a": []string{"ignored"},
			"b": []string{"b"},
			"c": []string{"c1", "c2"},
			"d": []string{"ignored"},
			"e": []string{"ignored"},
		},
		http.Header{
			"b": []string{"b"},
			"c": []string{"c1", "c2"},
		},
		OnlyHeaders("b", "c"),
	))
}
