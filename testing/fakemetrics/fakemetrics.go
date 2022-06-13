package fakemetrics

import (
	"fmt"
	"sync"

	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type MetricCall struct {
	Metric   string
	Name     string
	Value    float64
	ValueInt int64
	Tags     []string
	Rate     float64
}

var CMPMetrics = gocmp.Options{
	cmpopts.EquateApprox(0, 10),
	cmpopts.SortSlices(func(x, y MetricCall) bool {
		const format = "%s|%s|%s"
		return fmt.Sprintf(format, x.Metric, x.Name, x.Tags) <
			fmt.Sprintf(format, y.Metric, y.Name, y.Tags)
	}),
}

type Provider struct {
	mu sync.RWMutex

	// mutable state
	calls []MetricCall
}

func (f *Provider) Calls() []MetricCall {
	f.mu.RLock()
	defer f.mu.RUnlock()

	calls := make([]MetricCall, len(f.calls))
	copy(calls, f.calls)
	return calls
}

func (f *Provider) TimeInMilliseconds(name string, value float64, tags []string, rate float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, MetricCall{Metric: "timer", Name: name, Value: value, Tags: tags, Rate: rate})
	return nil
}

func (f *Provider) Gauge(name string, value float64, tags []string, rate float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, MetricCall{Metric: "gauge", Name: name, Value: value, Tags: tags, Rate: rate})
	return nil
}

func (f *Provider) Count(name string, value int64, tags []string, rate float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, MetricCall{Metric: "count", Name: name, ValueInt: value, Tags: tags, Rate: rate})
	return nil
}

func (f *Provider) Histogram(name string, value float64, tags []string, rate float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, MetricCall{Metric: "histogram", Name: name, Value: value, Tags: tags, Rate: rate})
	return nil
}

func (f *Provider) Close() error {
	return nil
}
