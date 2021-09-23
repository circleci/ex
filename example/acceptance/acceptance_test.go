package acceptance

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/circleci/ex/testing/compiler"
)

var (
	apiTestBinary = os.Getenv("API_TEST_BINARY")
)

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

//nolint:funlen
func runTests(m *testing.M) (int, error) {
	ctx := context.Background()

	p := compiler.NewParallel(2)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &apiTestBinary,
		Name:   "api",
		Target: "..",
		Source: "github.com/circleci/ex/example/cmd/api",
	})

	err := p.Run(ctx)
	if err != nil {
		return 0, err
	}

	fmt.Printf("Using 'api' test binary: %q\n", apiTestBinary)

	seed := randomSeed()
	fmt.Printf("Using random seed: %v\n", seed)
	rand.Seed(seed)

	return m.Run(), nil
}

func randomSeed() int64 {
	seed := os.Getenv("TEST_RANDOM_SEED")
	if seed == "" {
		return time.Now().UnixNano()
	}
	value, err := strconv.ParseInt(seed, 10, 64)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "invalid seed %v: %s", seed, err)
		return time.Now().UnixNano()
	}
	return value
}
