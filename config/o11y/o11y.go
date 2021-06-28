package o11y

import (
	"context"
	"fmt"
	"os"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/rollbar/rollbar-go"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
)

type Config struct {
	Statsd           string
	RollbarToken     secret.String
	RollbarEnv       string
	HoneycombEnabled bool
	HoneycombDataset string
	HoneycombKey     secret.String
	SampleTraces     bool
	Format           string
	Version          string
	Service          string
	StatsNamespace   string

	// Optional
	Mode  string
	Debug bool
}

// Setup is some crazy hackery to handle the fact that the beeline lib uses a
// singleton shared client that we cant simply guard with sync once, since we are not
// coordinating the calls to close. Instead we route the setup through here where we
// can create the provider once and coordinate the closes.
// Other Approaches:
// 1. We could protect beeline init (from the races) and then not close beeline in dev mode
// or sync once the close call but that would still mean doing something like this interceptor.
//
// 2. A stand alone development stack launcher of some sort. This would probably be the most correct
// in terms of running like production, but it adds some complexity to developer testing.
//
// 3. Instead of this mess we would have to implement beeline ourselves in a way that has a single client.
// we could do that and attempt to upstream the PR, but this is a fair amount of effort.
//
// This small wrapper is only in the code init flow, and once seen and understood can be forgotten,
// so it was considered OK to keep for now.
//
// If the coordinator is nil (for example if DevInit is not called as per production) then we immediately
// defer to the real setup

func Setup(ctx context.Context, o Config) (context.Context, func(context.Context), error) {
	if coordinator == nil {
		return setup(ctx, o)
	}
	return coordinator.setup(ctx, o)
}

func setup(ctx context.Context, o Config) (context.Context, func(context.Context), error) {
	// Set up observability by creating our observable context
	honeyConfig, err := honeyComb(o)
	if err != nil {
		return nil, nil, err
	}

	hostname, _ := os.Hostname()

	if o.Statsd != "" {
		stats, err := statsd.New(o.Statsd)
		if err != nil {
			return nil, nil, err
		}
		stats.Namespace = o.StatsNamespace
		stats.Tags = []string{
			"service:" + o.Service,
			"version:" + o.Version,
			"hostname:" + hostname,
		}
		if o.Mode != "" {
			stats.Tags = append(stats.Tags, "mode:"+o.Mode)
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
		client := rollbar.NewAsync(string(o.RollbarToken), o.RollbarEnv, o.Version, hostname, "")
		client.SetEnabled(true)
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

func (p rollBarHoneycombProvider) RollBarClient() *rollbar.Client {
	return p.rollBarClient
}

func honeyComb(o Config) (honeycomb.Config, error) {
	conf := honeycomb.Config{
		Host:         "",
		Dataset:      o.HoneycombDataset,
		Key:          string(o.HoneycombKey),
		Format:       o.Format,
		SendTraces:   o.HoneycombEnabled,
		SampleTraces: o.SampleTraces,
		SampleKeyFunc: func(fields map[string]interface{}) string {
			return fmt.Sprintf("%s %s %d",
				fields["app.server_name"],
				fields["request.path"],
				fields["response.status_code"],
			)
		},
		Debug: o.Debug,
	}
	return conf, conf.Validate()
}
