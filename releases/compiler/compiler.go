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
	Name        string
	Target      string
	Source      string
	Environment []string

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
	// #nosec - this is fine
	cmd := exec.CommandContext(ctx, goBin, "build",
		"-ldflags="+c.ldFlags,
		"-o", path,
		work.Source,
	)
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
