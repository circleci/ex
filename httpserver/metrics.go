package httpserver

import (
	"context"
)

type MetricProducer interface {
	// MetricName The name for this group of metrics
	//(Name might be cleaner, but is much more likely to conflict in implementations)
	MetricName() string
	// Gauges are instantaneous name value pairs
	Gauges(context.Context) map[string]float64
}
