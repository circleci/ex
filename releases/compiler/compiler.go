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
			goos = e[len("GOOS="):]
		}
	}

	path := binaryPath(work.Name, c.baseDir, goos)
	goBin := goPath()

	args := func() []string {
		if work.WithCoverage {
			return []string{
				"test",
				"-coverpkg=./...",
				"-c",
				"-tags", "testrunmain",
				"-o", path,
				work.Source,
			}
		} else {
			return []string{
				"build",
				"-o", path,
				"-ldflags=" + c.ldFlags,
				work.Source,
			}
		}
	}()

	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, work.Environment...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", err
	}

	if work.Result != nil {
		*work.Result = path
	}

	return path, err
}

func goPath() string {
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		return filepath.Join(goroot, "bin", "go")
	}
	return "go"
}

func binaryPath(name, tempDir, goos string) string {
	path := filepath.Join(tempDir, name)
	if goos == "windows" {
		return path + ".exe"
	}
	return path
}
