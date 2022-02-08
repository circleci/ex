package compiler

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type Compiler struct {
	dir string
}

func New() *Compiler {
	tempDir, err := ioutil.TempDir("", "acceptance-tests")
	if err != nil {
		panic(err)
	}

	return &Compiler{
		dir: tempDir,
	}
}

func (c *Compiler) Dir() string {
	return c.dir
}

func (c *Compiler) Cleanup() {
	_ = os.RemoveAll(c.dir)
}

type Work struct {
	Name        string
	Target      string
	Source      string
	Environment []string

	Result *string
}

// Compile a binary for testing. target is the path to the main package.
func (c *Compiler) Compile(ctx context.Context, work Work) (string, error) {
	cwd, err := filepath.Abs(work.Target)
	if err != nil {
		return "", err
	}

	path := binaryPath(work.Name, c.dir)
	goBin := goPath()
	// #nosec - this is fine
	cmd := exec.CommandContext(ctx, goBin, "build",
		"-ldflags=-w -s",
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

func binaryPath(name, tempDir string) string {
	path := filepath.Join(tempDir, name)
	if runtime.GOOS == "windows" {
		return path + ".exe"
	}
	return path
}
