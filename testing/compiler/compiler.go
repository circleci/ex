/*
Package binary provides a convenient mechanism for building and running a service binary
as part of an acceptance test suite.
*/
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

func New() (*Compiler, error) {
	tempDir, err := ioutil.TempDir("", "acceptance-tests")
	if err != nil {
		return nil, err
	}

	return &Compiler{
		dir: tempDir,
	}, nil
}

func (c *Compiler) Cleanup() {
	_ = os.RemoveAll(c.dir)
}

// Compile a binary for testing. target is the path to the main package.
func (c *Compiler) Compile(ctx context.Context, name, target string, source string) (string, error) {
	cwd, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	path := binaryPath(name, c.dir)
	goBin := goPath()
	// #nosec - this is fine
	cmd := exec.CommandContext(ctx, goBin, "test",
		"-coverpkg=./...",
		"-c",
		"-tags", "testrunmain",
		source,
		"-o", path)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return path, cmd.Run()
}

func goPath() string {
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		return "go"
	}
	return filepath.Join(filepath.Dir(goroot), "bin", "go")
}

func binaryPath(name, tempDir string) string {
	path := filepath.Join(tempDir, name)
	if runtime.GOOS == "windows" {
		return path + ".exe"
	}
	return path
}
