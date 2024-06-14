package o11y_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	o11yconfig "github.com/circleci/ex/config/o11y"
	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/testing/fakestatsd"
)

func TestO11Y_SecretRedacted(t *testing.T) {
	// confirm that honeycomb uses the json marshaller and that we dont see secrets
	buf := bytes.Buffer{}
	provider := honeycomb.New(honeycomb.Config{
		Writer: &buf,
	})
	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "secret test")
	s := secret.String("super-secret")
	span.AddField("secret", s)
	span.End()
	provider.Close(ctx)
	assert.Check(t, !strings.Contains(buf.String(), "super-secret"), buf.String())
	assert.Check(t, cmp.Contains(buf.String(), "REDACTED"))
}

func TestO11Y_SecretRedactedColor(t *testing.T) {
	// This test can be run to *VISUALLY* confirm that color honeycomb
	// formatter uses the json marshaller under the hood and that we dont see secrets.
	// Once you provide a writer the Format is ignored so this test does
	// not assert on anything since we cant catch the output
	//
	// The test is left in so that it can be eyeballed with -v
	// The other buffer based test does assert and it is very likely if secrets
	// leak in this test then the above test will fail in any case
	provider := honeycomb.New(honeycomb.Config{
		Format: "color",
	})
	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "secret test")
	s := secret.String("super-secret")
	span.AddField("secret", s)
	span.End()
	provider.Close(ctx)
}

func TestSetup_Wiring(t *testing.T) {
	s := fakestatsd.New(t)

	ctx := context.Background()
	ctx, cleanup, err := o11yconfig.Setup(ctx, o11yconfig.Config{
		Statsd:            s.Addr(),
		RollbarToken:      "qwertyuiop",
		RollbarDisabled:   true,
		RollbarEnv:        "production",
		RollbarServerRoot: "github.com/circleci/ex",
		HoneycombEnabled:  false,
		HoneycombDataset:  "does-not-exist",
		HoneycombKey:      "1234567890",
		SampleTraces:      false,
		Format:            "color",
		Version:           "1.2.3",
		Service:           "test-service",
		StatsNamespace:    "test.service",
		Mode:              "banana",
		Debug:             true,
	})
	assert.Assert(t, err)

	t.Run("Send metric", func(t *testing.T) {
		p := o11y.FromContext(ctx)
		err = p.MetricsProvider().Count("my_count", 1, []string{"mytag:myvalue"}, 1)
		assert.Check(t, err)
	})

	t.Run("Cleanup provider", func(t *testing.T) {
		cleanup(ctx)
	})

	t.Run("Check metrics received", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			metrics := s.Metrics()
			if len(metrics) == 0 {
				return poll.Continue("no metrics found yet")
			}
			return poll.Success()
		})

		metrics := s.Metrics()
		assert.Assert(t, cmp.Len(metrics, 1))
		metric := metrics[0]
		assert.Check(t, cmp.Equal("test.service.my_count", metric.Name))
		assert.Check(t, cmp.Equal("1|c|", metric.Value))
		assert.Check(t, cmp.Contains(metric.Tags, "service:test-service"))
		assert.Check(t, cmp.Contains(metric.Tags, "version:1.2.3"))
		assert.Check(t, cmp.Contains(metric.Tags, "mode:banana"))
		assert.Check(t, cmp.Contains(metric.Tags, "mytag:myvalue"))
	})
}

func TestSetup_WithWriter(t *testing.T) {
	buf := bytes.Buffer{}
	ctx := context.Background()
	ctx, cleanup, err := o11yconfig.Setup(ctx, o11yconfig.Config{
		Writer: &buf,
	})
	assert.Assert(t, err)
	defer cleanup(ctx)

	o11y.Log(ctx, "some log output")

	assert.Check(t, cmp.Contains(buf.String(), "some log output"))
}

func TestConfig_OTelSampleRates(t *testing.T) {
	conf := o11yconfig.Config{
		SampleRates: map[string]int{
			"foo": 128,
		},
	}
	otelSampleRates := conf.OtelSampleRates()

	assert.Check(t, cmp.DeepEqual(otelSampleRates, map[string]uint{
		"foo": uint(128),
	}))
}
