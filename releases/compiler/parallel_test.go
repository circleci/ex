package compiler

import (
	"os"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/golden"
	"gotest.tools/v3/icmd"

	"github.com/circleci/ex/testing/testcontext"
)

func TestParallel_Compile(t *testing.T) {
	tmpDir := fs.NewDir(t, "")
	c := New(Config{
		BaseDir:     tmpDir.Path(),
		LDFlags:     "-w -s",
		Parallelism: 2,
	})

	var binary1, binary2, binary3 string
	t.Cleanup(func() {
		c.Cleanup()
	})

	assert.Assert(t, t.Run("Compile binaries", func(t *testing.T) {
		c.Add(Work{
			Result:      &binary1,
			Name:        "binary1",
			Target:      "../..",
			Source:      "./releases/compiler/internal/cmd",
			Environment: []string{"FOO=foo1", "BAR=bar1"},
		})
		c.Add(Work{
			Result:      &binary2,
			Name:        "binary2",
			Target:      "../..",
			Source:      "./releases/compiler/internal/cmd2",
			Environment: []string{"FOO=foo2", "BAR=bar2"},
			Tags:        "work",
		})
		c.Add(Work{
			Result:       &binary3,
			Name:         "binary3",
			Target:       "../..",
			Source:       "./releases/compiler/internal/cmd3",
			Environment:  []string{"FOO=foo2", "BAR=bar2"},
			Tags:         "work",
			WithCoverage: true,
		})

		err := c.Run(testcontext.Background())
		assert.Check(t, err)

		_, err = os.Stat(binary1)
		assert.Check(t, err)

		_, err = os.Stat(binary2)
		assert.Check(t, err)

		_, err = os.Stat(binary3)
		assert.Check(t, err)
	}))

	t.Run("Run binaries", func(t *testing.T) {
		res := icmd.RunCommand(binary1, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 1: [arg1 arg2 arg3]",
		}))

		res = icmd.RunCommand(binary2, "arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 2: [arg1 arg2 arg3] correct",
		}))
	})

	t.Run("Run binary with coverage", func(t *testing.T) {
		// N.B. we use the txt extension here to not fall foul of the .out in .gitignore
		// we would expect instrumented binary runs to use .o or .out for coverage report extensions.
		file := fs.NewFile(t, "coverage.txt")
		res := icmd.RunCommand(binary3, "-test.run", "^TestRunMain$",
			"-test.coverprofile", file.Path(),
			"--",
			"arg1", "arg2", "arg3")
		assert.Check(t, res.Equal(icmd.Expected{
			Out: "command 3: [arg1 arg2 arg3] correct",
		}))
		assert.Check(t, golden.String(string(golden.Get(t, file.Path())), "coverage.txt"))
	})
}
