package system_test

import (
	"context"
	"errors"
	"log" //nolint:depguard // non-o11y log is allowed for a top-level fatal
	"time"

	"github.com/alecthomas/kong"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/httpserver/healthcheck"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/termination"
	"github.com/circleci/ex/testing/testcontext"
)

type cli struct {
	ShutdownDelay time.Duration `env:"SHUTDOWN_DELAY" default:"5s" help:"Delay shutdown by this amount" hidden:""`
	AdminAddr     string        `env:"ADMIN_ADDR" default:":8001" help:"The address for the admin API to listen on"`
	APIAddr       string        `env:"API_ADDR" default:":8000" help:"The address for the API to listen on"`
}

func ExampleSystem() {
	err := run()
	if err != nil && !errors.Is(err, termination.ErrTerminated) {
		log.Fatal("Unexpected Error: ", err)
	}
	log.Println("exited 0")
}

func run() (err error) {
	cli := cli{}
	kong.Parse(&cli)

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
