package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func newCompiler(baseDir, ldFlags string) *compiler {
	return &compiler{
		baseDir: baseDir,
		ldFlags: ldFlags,
	}
}

type compiler struct {
	baseDir string
	ldFlags string
}

func (c *compiler) Dir() string {
	return c.baseDir
}

type Work struct {
	Name         string
	Target       string
	Source       string
	WithCoverage bool
	Tags         string
	Environment  []string

	Result *string
}

// Compile a binary for testing. target is the path to the main package.
func (c *compiler) Compile(ctx context.Context, work Work) (string, error) {
	cwd, err := filepath.Abs(work.Target)
	if err != nil {
		return "", err
	}

	goos := runtime.GOOS
	for _, e := range work.Environment {
		if strings.HasPrefix(e, "GOOS=") {
			goos = strings.SplitN(e, "=", 2)[1]
		}
	}

	path := binaryPath(work.Name, c.baseDir, goos)
	goBin := goPath()
	var cmd *exec.Cmd
	if !work.WithCoverage {
		args := []string{
			"build",
			"-ldflags=" + c.ldFlags,
			"-o", path,
		}
		if work.Tags != "" {
			args = append(args, "-tags", work.Tags)
		}
		args = append(args, work.Source)
		// #nosec - this is fine
		cmd = exec.CommandContext(ctx, goBin, args...)
	} else {
		args := []string{
			"test",
			"-coverpkg=./...",
			"-c",
			work.Source,
			"-o", path,
			"-tags", "testrunmain",
		}
		if work.Tags != "" {
			args[len(args)-1] += " " + work.Tags
		}
		args = append(args, work.Source)
		// #nosec - this is fine
		cmd = exec.CommandContext(ctx, goBin, args...)
	}

	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, work.Environment...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return "", err
	}

	if work.Result != nil {
		*work.Result = path
	}
	return path, err
}

func goPath() string {
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		return "go"
	}
	return filepath.Join(goroot, "bin", "go")
}

func binaryPath(name, tempDir, goos string) string {
	path := filepath.Join(tempDir, name)
	if goos == "windows" {
		return path + ".exe"
	}
	return path
}
