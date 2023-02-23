package compiler

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

func TestParallel_Compile(t *testing.T) {
	tmpDir := fs.NewDir(t, "")
	c := New(Config{
		BaseDir:     tmpDir.Path(),
		LDFlags:     "-w -s",
		Parallelism: 2,
	})

	var binary1, binary2 string

	assert.Assert(t, t.Run("Compile binaries", func(t *testing.T) {
		err := c.Run(
			context.Background(),
			Work{
				Result:      &binary1,
				Name:        "binary1",
				Target:      "../..",
				Source:      "./testing/compiler/internal/cmd",
				Environment: []string{"FOO=foo1", "BAR=bar1"},
			},
			Work{
				Result:      &binary2,
				Name:        "binary2",
				Target:      "../..",
				Source:      "./testing/compiler/internal/cmd2",
				Environment: []string{"FOO=foo2", "BAR=bar2"},
			},
		)
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
