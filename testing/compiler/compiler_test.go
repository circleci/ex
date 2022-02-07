package compiler

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"
)

func TestCompiler_Compile(t *testing.T) {
	c := New()

	binary := ""
	t.Cleanup(func() {
		c.Cleanup()
		_, err := os.Stat(binary)
		assert.Check(t, os.IsNotExist(err))
	})

	assert.Assert(t, t.Run("Compile binary", func(t *testing.T) {
		var err error
		binary, err = c.Compile(context.Background(), Work{
			Name:        "name",
			Target:      "../..",
			Source:      "./testing/compiler/internal/cmd",
			Environment: []string{"FOO=foo", "BAR=bar"},
		})
		assert.Assert(t, err)
		_, err = os.Stat(binary)
		assert.Check(t, err)
	}))

	t.Run("Run binary", func(t *testing.T) {
		res := icmd.RunCommand(binary, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 1: [arg1 arg2 arg3]",
		}))
	})
}
