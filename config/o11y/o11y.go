package o11y

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/cenkalti/backoff/v4"
	"github.com/rollbar/rollbar-go"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
)

type Config struct {
	Statsd            string
	RollbarToken      secret.String
	RollbarEnv        string
	RollbarServerRoot string
	HoneycombEnabled  bool
	HoneycombDataset  string
	HoneycombHost     string
	HoneycombKey      secret.String
	SampleTraces      bool
	SampleKeyFunc     func(map[string]interface{}) string
	SampleRates       map[string]int
	Format            string
	Version           string
	Service           string
	StatsNamespace    string

	// Optional
	Mode                    string
	Debug                   bool
	RollbarDisabled         bool
	StatsdTelemetryDisabled bool
}

// Setup is the primary entrypoint to initialise the o11y system.
func Setup(ctx context.Context, o Config) (context.Context, func(context.Context), error) {
	honeyConfig, err := honeyComb(o)
	if err != nil {
		return nil, nil, err
	}

	hostname, _ := os.Hostname()

	if o.Statsd == "" {
		honeyConfig.Metrics = &statsd.NoOpClient{}
	} else {
		tags := []string{
			"service:" + o.Service,
			"version:" + o.Version,
			"hostname:" + hostname,
		}
		if o.Mode != "" {
			tags = append(tags, "mode:"+o.Mode)
		}

		statsdOpts := []statsd.Option{
			statsd.WithNamespace(o.StatsNamespace),
			statsd.WithTags(tags),
		}
		if o.StatsdTelemetryDisabled {
			statsdOpts = append(statsdOpts, statsd.WithoutTelemetry())
		}

		var stats *statsd.Client
		bo := backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 30)
		err := backoff.Retry(func() (err error) {
			stats, err = statsd.New(o.Statsd, statsdOpts...)
			return err
		}, backoff.WithContext(bo, ctx))
		if err != nil {
			return ctx, nil, err
		}

		honeyConfig.Metrics = stats
	}

	o11yProvider := honeycomb.New(honeyConfig)
	o11yProvider.AddGlobalField("service", o.Service)
	o11yProvider.AddGlobalField("version", o.Version)
	if o.Mode != "" {
		o11yProvider.AddGlobalField("mode", o.Mode)
	}

	if o.RollbarToken != "" {
		client := rollbar.NewAsync(o.RollbarToken.Value(), o.RollbarEnv, o.Version, hostname, o.RollbarServerRoot)
		client.SetEnabled(!o.RollbarDisabled)
		client.Message(rollbar.INFO, "Deployment")
		o11yProvider = rollBarHoneycombProvider{
			Provider:      o11yProvider,
			rollBarClient: client,
		}
	}

	ctx = o11y.WithProvider(ctx, o11yProvider)

	return ctx, o11yProvider.Close, nil
}

type rollBarHoneycombProvider struct {
	o11y.Provider
	rollBarClient *rollbar.Client
}

func (p rollBarHoneycombProvider) Close(ctx context.Context) {
	p.Provider.Close(ctx)
	_ = p.rollBarClient.Close()
}

func (p rollBarHoneycombProvider) RollBarClient() *rollbar.Client {
	return p.rollBarClient
}

func honeyComb(o Config) (honeycomb.Config, error) {
	if o.SampleKeyFunc == nil {
		o.SampleKeyFunc = func(fields map[string]interface{}) string {
			// defaults for gin server
			return fmt.Sprintf("%s %s %v",
				fields["http.server_name"],
				fields["http.route"],
				fields["http.status_code"],
			)
		}
	}

	conf := honeycomb.Config{
		Host:          o.HoneycombHost,
		Dataset:       o.HoneycombDataset,
		Key:           string(o.HoneycombKey),
		Format:        o.Format,
		SendTraces:    o.HoneycombEnabled,
		SampleTraces:  o.SampleTraces,
		SampleKeyFunc: o.SampleKeyFunc,
		SampleRates:   o.SampleRates,
		ServiceName:   o.Service,
		Debug:         o.Debug,
	}
	return conf, conf.Validate()
}
