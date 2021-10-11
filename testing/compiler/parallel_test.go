package compiler

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"
)

func TestParallel_Compile(t *testing.T) {
	c := NewParallel(2)

	var binary1, binary2 string
	t.Cleanup(func() {
		c.Cleanup()

		_, err := os.Stat(binary1)
		assert.Check(t, os.IsNotExist(err))
		_, err = os.Stat(binary2)
		assert.Check(t, os.IsNotExist(err))
	})

	assert.Assert(t, t.Run("Compile binaries", func(t *testing.T) {
		c.Add(Work{
			Result: &binary1,
			Name:   "binary1",
			Target: "../..",
			Source: "./testing/compiler/internal/cmd",
		})
		c.Add(Work{
			Result: &binary2,
			Name:   "binary2",
			Target: "../..",
			Source: "./testing/compiler/internal/cmd2",
		})

		err := c.Run(context.Background())
		assert.Check(t, err)
		_, err = os.Stat(binary1)
		assert.Check(t, err)
		_, err = os.Stat(binary2)
		assert.Check(t, err)
	}))

	t.Run("Run binaries", func(t *testing.T) {
		res := icmd.RunCommand(binary1, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 1: [arg1 arg2 arg3]",
		}))

		res = icmd.RunCommand(binary2, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 2: [arg1 arg2 arg3]",
		}))
	})
}
