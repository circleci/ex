package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/closer"
	"github.com/circleci/ex/internal/syncbuffer"
)

type Runner struct {
	baseEnv     []string
	dynamicEnv  func() []string
	coverageDir string

	mu       sync.Mutex
	cleanups []func() error
}

func New(baseEnv ...string) *Runner {
	return &Runner{
		baseEnv: baseEnv,
	}
}

func NewWithDynamicEnv(baseEnv []string, dynamicEnv func() []string) *Runner {
	return &Runner{
		baseEnv:    baseEnv,
		dynamicEnv: dynamicEnv,
	}
}

// CoverageReportDir tells the runner where to attempt to output a coverage report.
// A report will be generated in the given directory using the name of the binary, and the extension .out
func (r *Runner) CoverageReportDir(path string) {
	r.coverageDir = path
}

// Run starts the output service and waits a few seconds to confirm it has started
// successfully. The caller is responsible for calling Runner.Stop.
// If serverName is not empty then the server port will be detected using the standard
// server output as expected when running an ex httpserver.
// If it is left blank then only the admin server is detected.
// For custom timeouts whilst waiting for readiness use Start and result.Ready
func (r *Runner) Run(serverName, binary string, extraEnv ...string) (*Result, error) {
	result, err := r.Start(binary, extraEnv...)
	if err != nil {
		return nil, err
	}

	err = result.Ready(serverName, time.Second*20)
	if err != nil {
		_ = result.Stop()
		return result, err
	}

	r.addStop(result.Stop)

	return result, nil
}

// Start the `output` service, returning a buffer which contains the logs (stderr)
// of the process.
// The caller is responsible for calling Result.Stop and Runner.Stop.
func (r *Runner) Start(binary string, extraEnv ...string) (*Result, error) {
	var args []string
	if r.coverageDir != "" {
		args = []string{
			"-test.run", "^TestRunMain$",
			"-test.coverprofile", path.Join(r.coverageDir, path.Base(binary)) + ".out",
			"--",
		}
	}

	//#nosec:G204 // this is intentionally running a command for tests
	cmd := exec.Command(binary, args...)

	// Add base environment
	cmd.Env = make([]string, len(r.baseEnv))
	copy(cmd.Env, r.baseEnv)

	// Add dynamic environment
	if r.dynamicEnv != nil {
		cmd.Env = append(cmd.Env, r.dynamicEnv()...)
	}

	// Add extra environment
	cmd.Env = append(cmd.Env, extraEnv...)

	result := &Result{
		cmd:  cmd,
		logs: &syncbuffer.SyncBuffer{},
	}
	cmd.Stdout = io.MultiWriter(result.logs, os.Stdout)
	cmd.Stderr = io.MultiWriter(result.logs, os.Stderr)

	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start: %w", err)
	}
	return result, nil
}

func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	g, _ := errgroup.WithContext(context.Background())
	for _, cleanup := range r.cleanups {
		g.Go(cleanup)
	}

	r.cleanups = nil

	return g.Wait()
}

func (r *Runner) addStop(stop func() error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cleanups = append(r.cleanups, stop)
}

type Result struct {
	cmd  *exec.Cmd
	logs *syncbuffer.SyncBuffer

	apiPort   int
	adminPort int
}

// Ready will return nil if ports have been detected and the server is reporting readiness.
// If serverName is not empty then the server port will be detected using the standard
// server output as expected when running an ex httpserver.
// If it is empty then only the admin server is detected.
func (r *Result) Ready(serverName string, duration time.Duration) error {
	timeout := time.After(duration)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout hit for server %q after %v", serverName, duration)
		case <-time.After(20 * time.Millisecond):
		}
		if getPorts(r, serverName) {
			break
		}
	}

	// once the service is up and reporting ports we expect the readiness check to happen quite soon.
	duration = duration / 2
	timeout = time.After(duration)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("readiness timeout hit after %v", duration)
		case <-time.After(100 * time.Millisecond):
		}

		err := getReady(r.adminPort)
		if err != nil {
			fmt.Printf("readiness failure: %v\n", err)
		} else {
			break
		}
	}
	return nil
}

func (r *Result) Logs() string {
	return r.logs.String()
}

func (r *Result) APIAddr() string {
	return fmt.Sprintf("http://localhost:%v", r.apiPort)
}

func (r *Result) AdminAddr() string {
	return fmt.Sprintf("http://localhost:%v", r.adminPort)
}

// Stop the process, returning the exit error.
func (r *Result) Stop() error {
	// works around issue in go json test output: https://github.com/golang/go/issues/38063
	defer fmt.Println("sub-process stopped")

	// Windows does not support SIGINT
	if runtime.GOOS == "windows" {
		return kill(r.cmd)
	}

	err := r.cmd.Process.Signal(os.Interrupt)
	if err != nil {
		return fmt.Errorf("failed to SIGINT: %w", err)
	}

	select {
	case <-time.After(11 * time.Second):
		r.cmd.Process.Kill() //nolint: errcheck
		return fmt.Errorf("SIGINT timed out: %w", err)
	case err := <-r.Wait():
		return err
	}
}

func kill(cmd *exec.Cmd) error {
	err := cmd.Process.Kill()
	if err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}
	return nil
}

// Wait for the process to exit in a goroutine. The exit error is returned on the
// channel when the process exits.
func (r *Result) Wait() chan error {
	ch := make(chan error)
	go func() {
		err := r.cmd.Wait()
		ch <- err
		close(ch)
	}()
	return ch
}

func getPorts(r *Result, serviceName string) bool {
	lines := strings.Split(r.logs.String(), "\n")

	admin := getPort(lines, "admin", "")
	if admin == "" {
		return false
	}
	r.adminPort, _ = strconv.Atoi(admin)

	if serviceName == "" {
		return true
	}
	api := getPort(lines, serviceName, "")
	if api == "" {
		return false
	}
	r.apiPort, _ = strconv.Atoi(api)
	return true
}

var portRegexp = regexp.MustCompile(`app.address=(127\.0\.0\.1|\[::]):(\d+)`)

func getPort(lines []string, serverName, ignore string) string {
	for _, l := range lines {
		if !strings.Contains(l, "server: new-server "+serverName) {
			continue
		}
		matches := portRegexp.FindStringSubmatch(l)
		if len(matches) > 2 && matches[2] != ignore {
			return matches[2]
		}
	}
	return ""
}

func getReady(port int) (err error) {
	//nolint:bodyclose // handled by closer
	r, err := http.Get(fmt.Sprintf("http://localhost:%d/ready", port))
	if err != nil {
		return err
	}
	defer closer.ErrorHandler(r.Body, &err)
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("got non 200 response: %d: %s", r.StatusCode, b)
	}
	return nil
}
