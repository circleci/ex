package o11y

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/cenkalti/backoff/v4"
	"github.com/rollbar/rollbar-go"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/otel"
)

// OtelConfig contains all the things we need to configure for otel based instrumentation.
type OtelConfig struct {
	GrpcHostAndPort string
	HTTPHostAndPort string

	// HTTPAuthorization is the authorization token to send with http requests
	HTTPAuthorization secret.String

	Dataset string

	// DisableText prevents output to stdout for noisy services. Ignored if no other no hosts are supplied
	DisableText bool

	Test bool

	SampleTraces  bool
	SampleKeyFunc func(map[string]interface{}) string
	SampleRates   map[string]uint

	Statsd                  string
	StatsNamespace          string
	StatsdTelemetryDisabled bool

	RollbarToken      secret.String
	RollbarEnv        string
	RollbarServerRoot string
	RollbarDisabled   bool

	Version string
	Service string
	Mode    string
}

// Otel is the primary entrypoint to initialize the o11y system for otel.
func Otel(ctx context.Context, o OtelConfig) (context.Context, func(context.Context), error) {
	hostname, _ := os.Hostname()

	mProv, err := metricsProvider(ctx, o, hostname)
	if err != nil {
		return ctx, nil, fmt.Errorf("metrics provider failed: %w", err)
	}

	cfg := otel.Config{
		GrpcHostAndPort:   o.GrpcHostAndPort,
		HTTPHostAndPort:   o.HTTPHostAndPort,
		HTTPAuthorization: o.HTTPAuthorization,
		Dataset:           o.Dataset,
		ResourceAttributes: []attribute.KeyValue{
			semconv.ServiceNameKey.String(o.Service),
			semconv.ServiceVersionKey.String(o.Version),
			// Other Config specific fields
			attribute.String("service.mode", o.Mode),

			// HC Backwards compatible fields - can remove once boards are updated
			attribute.String("service", o.Service),
			attribute.String("mode", o.Mode),
			attribute.String("version", o.Version),
		},

		DisableText: o.DisableText,

		SampleTraces:  o.SampleTraces,
		SampleKeyFunc: o.SampleKeyFunc,
		SampleRates:   o.SampleRates,

		Test: o.Test,

		Metrics: mProv,
	}

	o11yProvider, err := otel.New(cfg)
	if err != nil {
		return ctx, nil, err
	}

	o11yProvider.AddGlobalField("service", o.Service)
	o11yProvider.AddGlobalField("version", o.Version)
	if o.Mode != "" {
		o11yProvider.AddGlobalField("mode", o.Mode)
	}

	if o.RollbarToken != "" {
		client := rollbar.NewAsync(o.RollbarToken.Raw(), o.RollbarEnv, o.Version, hostname, o.RollbarServerRoot)
		client.SetEnabled(!o.RollbarDisabled)
		client.Message(rollbar.INFO, "Deployment")
		o11yProvider = rollbarOtelProvider{
			Provider:      o11yProvider,
			rollBarClient: client,
		}
	}

	ctx = o11y.WithProvider(ctx, o11yProvider)

	return ctx, o11yProvider.Close, nil
}

// N.B this copies the block from Setup, but don't factor that out since the HC stuff will be removed soon
// TODO - delete this comment after HC cleanup
func metricsProvider(ctx context.Context, o OtelConfig, hostname string) (o11y.ClosableMetricsProvider, error) {
	if o.Statsd == "" {
		return &statsd.NoOpClient{}, nil
	}

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
		return nil, err
	}
	return stats, nil
}

type rollbarOtelProvider struct {
	o11y.Provider
	rollBarClient *rollbar.Client
}

func (p rollbarOtelProvider) Close(ctx context.Context) {
	p.Provider.Close(ctx)
	_ = p.rollBarClient.Close()
}

func (p rollbarOtelProvider) RollBarClient() *rollbar.Client {
	return p.rollBarClient
}

func (p rollbarOtelProvider) RawProvider() *otel.Provider {
	return p.Provider.(*otel.Provider)
}
