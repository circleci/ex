package kong

import (
	"context"
	"os"

	"github.com/alecthomas/kong"

	"github.com/circleci/ex/kong/vault"
)

func ParseCLIWithVault(cli any, cfg vault.Config) error {
	if cfg.Host != "" {
		resolver, err := vault.New(context.Background(), cfg)
		if err != nil {
			return err
		}
		parser, err := kong.New(cli, kong.Resolvers(resolver))
		if err != nil {
			return err
		}
		_, err = parser.Parse(os.Args[1:])
		if err != nil {
			return err
		}
	} else {
		kong.Parse(cli)
	}
	return nil
}
