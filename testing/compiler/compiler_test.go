package compiler

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/golden"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

func TestCompiler_Compile(t *testing.T) {
	c, err := New()
	assert.Assert(t, err)
	t.Cleanup(c.Cleanup)

	binary := ""
	assert.Assert(t, t.Run("Compile binary", func(t *testing.T) {
		var err error
		binary, err = c.Compile(context.Background(), "name", "../..", "./testing/compiler/internal/cmd")
		assert.Assert(t, err)
		_, err = os.Stat(binary)
		assert.Check(t, err)
	}))

	t.Run("Run binary", func(t *testing.T) {
		file := fs.NewFile(t, "coverage.txt")
		res := icmd.RunCommand(binary, "-test.run", "^TestRunMain$",
			"-test.coverprofile", file.Path(),
			"--",
			"arg1", "arg2", "arg3",
		)
		assert.Check(t, res.Equal(icmd.Expected{
			ExitCode: 0,
			Out:      "[arg1 arg2 arg3]",
		}))
		assert.Check(t, golden.String(string(golden.Get(t, file.Path())), "coverage.txt"))
	})

	t.Run("Delete binary", func(t *testing.T) {
		c.Cleanup()
		_, err = os.Stat(binary)
		assert.Check(t, os.IsNotExist(err))
	})
}
