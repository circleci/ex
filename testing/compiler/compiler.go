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
	cmd := exec.CommandContext(ctx, goBin, "build",
		"-o", path,
		source,
	)
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
	return filepath.Join(goroot, "bin", "go")
}

func binaryPath(name, tempDir string) string {
	path := filepath.Join(tempDir, name)
	if runtime.GOOS == "windows" {
		return path + ".exe"
	}
	return path
}
