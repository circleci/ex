package o11y_test

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	o11yconfig "github.com/circleci/ex/config/o11y"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/fakestatsd"
)

func TestSetup(t *testing.T) {
	s := fakestatsd.New(t)

	ctx := context.Background()
	ctx, cleanup, err := o11yconfig.Otel(ctx, o11yconfig.OtelConfig{
		Statsd:            s.Addr(),
		RollbarToken:      "qwertyuiop",
		RollbarDisabled:   true,
		RollbarEnv:        "production",
		RollbarServerRoot: "github.com/circleci/ex",
		SampleTraces:      false,
		Test:              true,
		Version:           "1.2.3",
		Service:           "test-service",
		StatsNamespace:    "test.service",
		Mode:              "banana",
	})
	assert.Assert(t, err)

	t.Run("Add some telemetry", func(t *testing.T) {
		p := o11y.FromContext(ctx)
		err = p.MetricsProvider().Count("my_count", 1, []string{"mytag:myvalue"}, 1)
		assert.Check(t, err)

		_, sp := p.StartSpan(ctx, "my_span")
		sp.End()
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
