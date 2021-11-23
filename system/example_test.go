package system_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/httpserver/healthcheck"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/termination"
	"github.com/circleci/ex/testing/testcontext"
)

type cli struct {
	ShutdownDelay time.Duration
	AdminAddr     string
	APIAddr       string
}

func ExampleSystem() {
	err := run()
	if err != nil && !errors.Is(err, termination.ErrTerminated) {
		fmt.Println("Unexpected Error: ", err)
		os.Exit(1)
	}
	fmt.Println("exited 0")

	// output: exited 0
}

func run() (err error) {
	cli := cli{}
	flag.DurationVar(&cli.ShutdownDelay, "shutdown-delay", 5*time.Second, "Delay shutdown by this amount")
	flag.StringVar(&cli.AdminAddr, "admin-addr", ":8001", "The address for the admin API to listen on")
	flag.StringVar(&cli.APIAddr, "api-addr", ":8000", "The address for the API to listen on")
	flag.Parse()

	// Use a properly wired o11y in a real application
	ctx := testcontext.Background()

	sys := system.New()
	defer sys.Cleanup(ctx)

	err = loadAPI(ctx, cli, sys)
	if err != nil {
		return err
	}

	// Should be last so it collects all the health checks
	_, err = healthcheck.Load(ctx, cli.AdminAddr, sys)
	if err != nil {
		return err
	}

	return sys.Run(ctx, cli.ShutdownDelay)
}

func loadAPI(ctx context.Context, cli cli, sys *system.System) error {
	r := ginrouter.Default(ctx, "api")

	_, err := httpserver.Load(ctx, "api", cli.APIAddr, r, sys)
	return err
}
