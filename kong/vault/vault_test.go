package vault

import (
	"context"
	"fmt"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/hashicorp/vault/api"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/testing/testcontext"
)

const (
	host      = "0.0.0.0"
	port      = 8200
	rootToken = "dummyroot"
)

func TestVaultResolve(t *testing.T) {
	ctx := testcontext.Background()

	var cli struct {
		Rm struct {
			Force     bool     `help:"Force removal." env:"FORCE"`
			Recursive string   `help:"Recursively remove files." env:"RECURSIVE" default:"iterate"`
			Optional  []string `help:"Paths to remove." env:"OPTIONAL"`

			Paths []string `arg:"" name:"path" help:"Paths to remove." type:"path"`
		} `cmd:"" help:"Remove files."`

		Ls struct {
			Paths []string `arg:"" optional:"" name:"path" help:"Paths to list." type:"path"`
		} `cmd:"" help:"List paths."`
	}

	// This should be overridden if set in vault
	t.Setenv("RECURSIVE", "loop")

	t.Run("env-still-used", func(t *testing.T) {
		seedVault(t, "test-secret-v1", map[string]any{
			"FORCE": "true",
			// Don't set RECURSIVE so we see the env value
			"OPTIONAL": []string{"foo", "bar", "baz"},
		})

		resolver, err := New(ctx, Config{
			DisableTLS: true,
			Host:       host,
			Port:       port,
			SecretName: "test-secret-v1",
			Token:      rootToken,
		})
		assert.Assert(t, err)
		parser, err := kong.New(&cli, kong.Resolvers(resolver))
		assert.Assert(t, err)

		_, err = parser.Parse([]string{"rm", "root"})
		assert.Assert(t, err)

		assert.Check(t, cmp.DeepEqual(cli.Rm.Force, true))
		assert.Check(t, cmp.DeepEqual(cli.Rm.Recursive, "loop"))
		assert.Check(t, cmp.DeepEqual(cli.Rm.Optional, []string{"foo", "bar", "baz"}))
	})

	t.Run("vault-used", func(t *testing.T) {
		seedVault(t, "test-secret-v1", map[string]any{
			"FORCE":     "true",
			"RECURSIVE": "recur",
			"OPTIONAL":  "foo,bar,baz",
		})

		t.Run("good-secret-name", func(t *testing.T) {
			resolver, err := New(ctx, Config{
				DisableTLS: true,
				Host:       host,
				Port:       port,
				SecretName: "test-secret-v1",
				Token:      rootToken,
			})
			assert.Assert(t, err)
			parser, err := kong.New(&cli, kong.Resolvers(resolver))
			assert.Assert(t, err)

			_, err = parser.Parse([]string{"rm", "root"})
			assert.Assert(t, err)

			assert.Check(t, cmp.DeepEqual(cli.Rm.Force, true))
			assert.Check(t, cmp.DeepEqual(cli.Rm.Recursive, "recur"))
			assert.Check(t, cmp.DeepEqual(cli.Rm.Optional, []string{"foo", "bar", "baz"}))
		})

		t.Run("bad-secret-name", func(t *testing.T) {
			_, err := New(ctx, Config{
				DisableTLS: true,
				Host:       host,
				Port:       port,
				SecretName: "bad-test-secret-v1",
				Token:      rootToken,
			})
			assert.ErrorContains(t, err, "secret not found")
		})
	})
}

func seedVault(t *testing.T, secretName string, data map[string]any) {
	cl := rootClient(t)
	_, err := cl.KVv2(defaultSecretMount).Put(context.Background(), secretName, data)
	assert.Assert(t, err)
}

func rootClient(t *testing.T) *api.Client {
	c := api.DefaultConfig()
	c.Address = fmt.Sprintf("http://%s:%d", host, port)
	client, err := api.NewClient(c)
	assert.Assert(t, err)
	client.SetToken(rootToken)
	client.Auth()
	return client
}
