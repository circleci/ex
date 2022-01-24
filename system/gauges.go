package system

import (
	"context"
	"fmt"
	"strings"

	"github.com/circleci/ex/o11y"
)

type GaugeProducer interface {
	// GaugeName The name for this group of metrics
	//(Name might be cleaner, but is much more likely to conflict in implementations)
	GaugeName() string
	// Gauges are instantaneous name value pairs
	Gauges(context.Context) map[string][]TaggedValue
}

type TaggedValue struct {
	Val  float32
	Tags []string
}

func emitGauges(ctx context.Context, producers []GaugeProducer) {
	metrics := o11y.FromContext(ctx).MetricsProvider()
	for _, producer := range producers {
		emitGauge(ctx, metrics, producer)
	}
}

func emitGauge(ctx context.Context, provider o11y.MetricsProvider, producer GaugeProducer) {
	producerName := strings.Replace(producer.GaugeName(), "-", "_", -1)
	for f, tvs := range producer.Gauges(ctx) {
		for _, tv := range tvs {
			scopedField := fmt.Sprintf("gauge.%s.%s", producerName, f)
			_ = provider.Gauge(scopedField, float64(tv.Val), tv.Tags, 1)
		}
	}
}
