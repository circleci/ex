package kongtest

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/golden"
)

func TestHelp(t *testing.T) {
	type cli struct {
		StringVar   string        `default:"string-default" env:"STRING_VAR"`
		IntVar      int           `default:"123" env:"INT_VAR"`
		BoolVar     bool          `default:"true" env:"BOOL_VAR"`
		DurationVar time.Duration `default:"10s" env:"DURATION_VAR"`
	}

	c := cli{}
	s := Help(t, &c)
	assert.Check(t, golden.String(s, "help.txt"))
	assert.Check(t, cmp.DeepEqual(c, cli{
		StringVar:   "string-default",
		IntVar:      123,
		BoolVar:     true,
		DurationVar: 10 * time.Second,
	}))
}
