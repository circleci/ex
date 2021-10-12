package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/circleci/ex/internal/syncbuffer"
)

type Runner struct {
	baseEnv    []string
	dynamicEnv func() []string

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

// Run starts the output service and waits a few seconds to confirm it has started
// successfully. The caller is responsible for calling Stop.
func (r *Runner) Run(serverName, binary string, extraEnv ...string) (*Result, error) {
	result, err := r.Start(binary, extraEnv...)
	if err != nil {
		return nil, err
	}

	gotPorts := false
	launched := false

	defer func() {
		if !gotPorts || !launched {
			_ = result.Stop()
		}
	}()

	timeout := time.After(20 * time.Second)
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout hit after %s", 20*time.Second)
		case <-time.After(20 * time.Millisecond):
		}
		if getPorts(result, serverName) {
			gotPorts = true
			break
		}
	}

	timeout = time.After(10 * time.Second)
	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("timeout hit after %s", 10*time.Second)
		case <-time.After(20 * time.Millisecond):
		}

		err := getReady(result.adminPort)
		if err != nil {
			fmt.Printf("readiness failure: %v\n", err)
		} else {
			launched = true
			break
		}
	}

	r.addStop(result.Stop)

	return result, nil
}

// Start the `output` service, returning a buffer which contains the logs (stderr)
// of the process.
// The caller is responsible for calling Stop.
func (r *Runner) Start(binary string, extraEnv ...string) (*Result, error) {
	//#nosec:G204 // this is intentionally running a command for tests
	cmd := exec.Command(binary)

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
	api := getPort(lines, serviceName, "")
	r.adminPort, _ = strconv.Atoi(admin)
	r.apiPort, _ = strconv.Atoi(api)
	return true
}

var portRegexp = regexp.MustCompile(`app.address=127.0.0.1:(\d+)`)

func getPort(lines []string, serverName, ignore string) string {
	for _, l := range lines {
		if !strings.Contains(l, "server: new-server "+serverName) {
			continue
		}
		matches := portRegexp.FindStringSubmatch(l)
		if len(matches) > 1 && matches[1] != ignore {
			return matches[1]
		}
	}
	return ""
}

func getReady(port int) error {
	r, err := http.Get(fmt.Sprintf("http://localhost:%d/ready", port))
	if err != nil {
		return err
	}
	defer r.Body.Close() // not concerned about an error here
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("got non 200 response: %d: %s", r.StatusCode, b)
	}
	return nil
}
