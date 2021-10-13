package main

import (
	"context"
	"errors"
	"log" //nolint:depguard // non-o11y log is allowed for a top-level fatal
	"time"

	"github.com/alecthomas/kong"
	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/healthcheck"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/termination"

	/* todo rename 'ex/example' to 'your-service' */
	"github.com/circleci/ex/example/api/api"
	"github.com/circleci/ex/example/books"
	"github.com/circleci/ex/example/cmd"
	"github.com/circleci/ex/example/cmd/setup"
)

type cli struct {
	setup.CLI

	ShutdownDelay time.Duration `env:"SHUTDOWN_DELAY" default:"5s" help:"Delay shutdown by this amount" hidden:""`
	APIAddr       string        `env:"API_ADDR" default:":8000" help:"The address for the API to listen on"`

	/* other specific env vars in here*/
}

func main() {
	err := run(cmd.Version, cmd.Date)
	if err != nil && !errors.Is(err, termination.ErrTerminated) {
		log.Fatal("Unexpected Error: ", err)
	}
	log.Println("exited 0")
}

func run(version, date string) (err error) {
	cli := cli{}
	kong.Parse(&cli)

	ctx, o11yCleanup, err := setup.LoadO11y(version, "api", cli.CLI)
	if err != nil {
		return err
	}
	defer o11yCleanup(ctx)

	ctx, runSpan := o11y.StartSpan(ctx, "main: run")
	defer o11y.End(runSpan, &err)

	o11y.Log(ctx, "starting api",
		o11y.Field("version", version),
		o11y.Field("date", date),
	)

	sys := system.New()
	defer sys.Cleanup(ctx)

	/* load other things in here that the API might need*/

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
	txm, err := setup.LoadTxManager(ctx, cli.CLI, sys)
	if err != nil {
		return err
	}

	a := api.New(ctx, api.Options{
		Store: books.NewStore(txm),
	})

	_, err = httpserver.Load(ctx, "api", cli.APIAddr, a.Handler(), sys)
	return err
}
