// Package setup contains common wiring/setup code used by all services
package setup

import (
	"context"
	"fmt"
	"math/rand"
	"time"
	_ "time/tzdata" // include embedded timezone data

	"github.com/circleci/ex/config/o11y"
	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/db"
	"github.com/circleci/ex/rootcerts"
	"github.com/circleci/ex/system"
)

type CLI struct {
	AdminAddr string `env:"ADMIN_ADDR" default:":8001" help:"The address for the admin api to listen on"`

	O11yStatsd           string        `name:"o11y-statsd" env:"O11Y_STATSD" default:"metrics.kube-system.svc.cluster.local:8125" help:"Address to send statsd metrics"`
	O11yHoneycombEnabled bool          `name:"o11y-honeycomb" env:"O11Y_HONEYCOMB" default:"true" help:"Send traces to honeycomb"`
	O11yHoneycombDataset string        `name:"o11y-honeycomb-dataset" env:"O11Y_HONEYCOMB_DATASET" default:"execution"`
	O11yHoneycombKey     secret.String `name:"o11y-honeycomb-key" env:"O11Y_HONEYCOMB_KEY"`
	O11yFormat           string        `name:"o11y-format" env:"O11Y_FORMAT" enum:"json,color,text" default:"json" help:"Format used for stderr logging"`
	O11yRollbarToken     secret.String `name:"o11y-rollbar-token" env:"O11Y_ROLLBAR_TOKEN"`
	O11yRollbarEnv       string        `name:"o11y-rollbar-env" env:"O11Y_ROLLBAR_ENV" default:"production"`

	DBHost     string        `env:"DB_HOST" default:"example.db.infra.circleci.com"`
	DBPort     int           `env:"DB_PORT" default:"5432"`
	DBUser     string        `env:"DB_USER" default:"example"`
	DBPassword secret.String `env:"DB_PASSWORD"`
	DBName     string        `env:"DB_NAME" default:"example"`
	DBSSL      bool          `env:"DB_SSL" name:"db-ssl" default:"true"`
}

func init() {
	rand.Seed(time.Now().Unix())
	err := rootcerts.UpdateDefaultTransport()
	if err != nil {
		panic(fmt.Errorf("failed to inject rootcerts: %w", err))
	}
}

func LoadO11y(version, mode string, cli CLI) (context.Context, func(context.Context), error) {
	cfg := o11y.Config{
		Statsd:            cli.O11yStatsd,
		RollbarToken:      cli.O11yRollbarToken,
		RollbarEnv:        cli.O11yRollbarEnv,
		RollbarServerRoot: "github.com/circleci/ex/example",
		HoneycombEnabled:  cli.O11yHoneycombEnabled,
		HoneycombDataset:  cli.O11yHoneycombDataset,
		HoneycombKey:      cli.O11yHoneycombKey,
		Format:            cli.O11yFormat,
		Version:           version,
		Service:           "example",
		StatsNamespace:    "circleci.example.",
		Mode:              mode,
	}
	return o11y.Setup(context.Background(), cfg)
}

func LoadTxManager(ctx context.Context, cli CLI, sys *system.System) (*db.TxManager, error) {
	return db.Load(ctx, "example", "example", db.Config{
		Host: cli.DBHost,
		Port: cli.DBPort,
		User: cli.DBUser,
		Pass: cli.DBPassword,
		Name: cli.DBName,
		SSL:  cli.DBSSL,
	}, sys)
}
