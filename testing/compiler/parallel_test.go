package compiler

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/golden"
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
			Result:      &binary1,
			Name:        "binary1",
			Target:      "../..",
			Source:      "./testing/compiler/internal/cmd",
			Environment: []string{"FOO=foo1", "BAR=bar1"},
		})
		c.Add(Work{
			Result:      &binary2,
			Name:        "binary2",
			Target:      "../..",
			Source:      "./testing/compiler/internal/cmd2",
			Environment: []string{"FOO=foo2", "BAR=bar2"},
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

func TestParallel_Compile_WithCoverage(t *testing.T) {
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
			Result:       &binary1,
			Name:         "binary1",
			Target:       "../..",
			Source:       "./testing/compiler/internal/cmd",
			WithCoverage: true,
			Environment:  []string{"FOO=foo1", "BAR=bar1"},
		})
		c.Add(Work{
			Result:      &binary2,
			Name:        "binary2",
			Target:      "../..",
			Source:      "./testing/compiler/internal/cmd2",
			Environment: []string{"FOO=foo2", "BAR=bar2"},
		})

		err := c.Run(context.Background())
		assert.Check(t, err)
		_, err = os.Stat(binary1)
		assert.Check(t, err)
		_, err = os.Stat(binary2)
		assert.Check(t, err)
	}))

	t.Run("Run binaries", func(t *testing.T) {
		// N.B. we use the txt extension here to not fall foul of the .out in .gitignore
		// we would expect instrumented binary runs to use .o or .out for coverage report extensions.
		file := fs.NewFile(t, "coverage.txt")
		res := icmd.RunCommand(binary1, "-test.run", "^TestRunMain$",
			"-test.coverprofile", file.Path(),
			"--",
			"arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 1: [arg1 arg2 arg3]",
		}))
		assert.Check(t, golden.String(string(golden.Get(t, file.Path())), "coverage.txt"))

		res = icmd.RunCommand(binary2, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 2: [arg1 arg2 arg3]",
		}))
	})
}
