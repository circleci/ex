// Package vault provides a Kong CLI Resolver that will provide default flag values from Vault
package vault

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/alecthomas/kong"
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/kubernetes"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/rootcerts"
)

const (
	defaultServiceAccountRole = "default"
	defaultSecretMount        = "secret"
)

// Resolver is Kong resolver that can collect default flag values from Vault.
type Resolver struct {
	client  *api.Client
	secrets *api.KVSecret
}

type Config struct {
	DisableTLS bool
	Host       string
	Port       int
	SecretName string
	Token      secret.String
}

// New creates a new resolver, authenticating with the optional passed in token (used for local testing).
// If token is empty then K8s service account auth is attempted.
func New(ctx context.Context, cfg Config) (*Resolver, error) {
	c := api.DefaultConfig()
	proto := "http"
	if !cfg.DisableTLS {
		proto = "https"
		clientTLSConfig := c.HttpClient.Transport.(*http.Transport).TLSClientConfig
		clientTLSConfig.MinVersion = tls.VersionTLS12
		clientTLSConfig.RootCAs = rootcerts.ServerCertPool()
		clientTLSConfig.ServerName = cfg.Host
	}

	c.Address = fmt.Sprintf("%s://%s:%d", proto, cfg.Host, cfg.Port)
	client, err := api.NewClient(c)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	resolver := &Resolver{client: client}

	if cfg.Token != "" {
		resolver.client.SetToken(cfg.Token.Value())
	} else {
		err = resolver.k8sServiceAuth(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create authenticate vault client: %w", err)
		}
	}

	sec, err := resolver.client.KVv2(defaultSecretMount).Get(ctx, cfg.SecretName)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret:%q from vault : %w", cfg.SecretName, err)
	}

	resolver.secrets = sec

	return resolver, nil
}

func (r *Resolver) k8sServiceAuth(ctx context.Context) error {
	auth, err := kubernetes.NewKubernetesAuth(defaultServiceAccountRole)
	if err != nil {
		return fmt.Errorf("failed to create vault auth: %w", err)
	}
	_, err = r.client.Auth().Login(ctx, auth)
	if err != nil {
		return fmt.Errorf("failed to log into Vault: %w", err)
	}
	return nil
}

func (r *Resolver) Validate(_ *kong.Application) error {
	return nil
}

// Resolve resolves default values for any flag that has an env set. The hierarchy of commands is ignored, expecting
// that the env var was named uniquely in the config. Only one env is supported per flag
func (r *Resolver) Resolve(_ *kong.Context, _ *kong.Path, flag *kong.Flag) (interface{}, error) {
	if r.secrets == nil {
		return nil, nil
	}
	if len(flag.Envs) == 0 {
		return nil, nil
	}
	val := r.secrets.Data[flag.Envs[0]]

	return val, nil
}
