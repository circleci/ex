package httpmetrics

import (
	"testing"
	"time"

	"gotest.tools/v3/poll"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/testing/fakemetricrec"
	"github.com/circleci/ex/testing/testcontext"

	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestProvider_Record(t *testing.T) {

	tests := []struct {
		name string

		namespace           string
		metrics             []metricData
		expectedMetricsData []metricData
	}{
		{
			name: "records a metric",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
		},
		{
			name:      "records a metric with a namespace",
			namespace: "ex",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "ex.test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
		},
		{
			name: "records multiple metrics when tags are different",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"baz:bar"},
				},
			},
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"baz:bar"},
				},
			},
		},
		{
			name: "records multiple metrics when tags are the same",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar", "apple:banana"},
				},
				{
					Type:  "gauge",
					Name:  "test",
					Value: 2,
					Tags:  []string{"foo:bar", "apple:banana"},
				},
			},
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar", "apple:banana"},
				},
				{
					Type:  "gauge",
					Name:  "test",
					Value: 2,
					Tags:  []string{"foo:bar", "apple:banana"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := createProvider(Config{Namespace: tt.namespace})
			for _, aMet := range tt.metrics {
				m.record(aMet.Type, aMet.Name, aMet.Value, aMet.Tags)
			}

			assert.Check(t, cmp.DeepEqual(m.data, tt.expectedMetricsData))
		})
	}
}

func TestProvider_Publish(t *testing.T) {
	ctx := testcontext.Background()
	server := fakemetricrec.New(ctx)
	t.Cleanup(server.Close)

	tests := []struct {
		name    string
		metrics []metricData
		// secondaryMetrics holds metrics recorded while metrics were being published.
		secondaryMetrics    []metricData
		wantPublishErr      bool
		expectedMetricsData []metricData
	}{
		{
			name: "clears internal memory array after successful publish",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			expectedMetricsData: []metricData{},
		},
		{
			name: "metrics records during a successful publish are not lost",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			secondaryMetrics: []metricData{
				{
					Type:  "gauge",
					Name:  "latency",
					Value: 1,
					Tags:  []string{"apple:banana"},
				},
			},
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "latency",
					Value: 1,
					Tags:  []string{"apple:banana"},
				},
			},
		},
		{
			name: "metrics are rolled back when publish fails",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			secondaryMetrics: []metricData{
				{
					Type:  "gauge",
					Name:  "latency",
					Value: 1,
					Tags:  []string{"apple:banana"},
				},
			},
			wantPublishErr: true,
			expectedMetricsData: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
				{
					Type:  "gauge",
					Name:  "latency",
					Value: 1,
					Tags:  []string{"apple:banana"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				URL:        server.URL + "/metric",
				AuthToken:  secret.String("foo"),
				GlobalTags: nil,
			}
			m := createProvider(cfg)
			for _, aMet := range tt.metrics {
				m.record(aMet.Type, aMet.Name, aMet.Value, aMet.Tags)
			}

			server.SetErrOnMetrics(tt.wantPublishErr)

			eg := errgroup.Group{}
			eg.Go(func() error {
				m.Publish(ctx)
				return nil
			})

			eg.Go(func() error {
				time.Sleep(5 * time.Millisecond)
				for _, aMet := range tt.secondaryMetrics {
					m.record(aMet.Type, aMet.Name, aMet.Value, aMet.Tags)
				}
				return nil
			})

			err := eg.Wait()
			assert.NilError(t, err)

			assert.Check(t, cmp.DeepEqual(m.data, tt.expectedMetricsData))
		})
	}
}

func TestProvider_New(t *testing.T) {
	tests := []struct {
		name                string
		metrics             []metricData
		expectedMetricsData map[string]float64
	}{
		{
			name: "single metric",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
			},
			expectedMetricsData: map[string]float64{"test": 1},
		},
		{
			name: "many metric",
			metrics: []metricData{
				{
					Type:  "gauge",
					Name:  "test1",
					Value: 1,
					Tags:  []string{"foo:bar"},
				},
				{
					Type:  "gauge",
					Name:  "test2",
					Value: 2,
					Tags:  []string{"foo:baz"},
				},
				{
					Type:  "gauge",
					Name:  "test3",
					Value: 3,
					Tags:  []string{"foo:bat"},
				},
			},
			expectedMetricsData: map[string]float64{"test1": 1, "test2": 2, "test3": 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("publishes repeatable", func(t *testing.T) {
				ctx := testcontext.Background()
				server := fakemetricrec.New(ctx)
				t.Cleanup(server.Close)

				cfg := Config{
					URL:             server.URL + "/metric",
					AuthToken:       secret.String("foo"),
					GlobalTags:      nil,
					PublishInterval: 50 * time.Millisecond,
				}
				m := New(cfg)
				for i, aMet := range tt.metrics {
					m.record(aMet.Type, aMet.Name, aMet.Value, aMet.Tags)
					poll.WaitOn(t, func(t poll.LogT) poll.Result {
						if len(server.GetMetrics()) == (i + 1) {
							return poll.Success()
						}
						return poll.Continue("data still unsent")
					}, poll.WithTimeout(2*time.Second))
				}

				assert.Check(t, cmp.DeepEqual(server.GetMetrics(), tt.expectedMetricsData))
				assert.NilError(t, m.Close())
			})

			t.Run("publishes on close", func(t *testing.T) {
				ctx := testcontext.Background()
				server := fakemetricrec.New(ctx)
				t.Cleanup(server.Close)

				cfg := Config{
					URL:             server.URL + "/metric",
					AuthToken:       secret.String("foo"),
					GlobalTags:      nil,
					PublishInterval: 10 * time.Minute, // long to prevent publish before close
				}
				m := New(cfg)
				for _, aMet := range tt.metrics {
					m.record(aMet.Type, aMet.Name, aMet.Value, aMet.Tags)
				}
				assert.NilError(t, m.Close())

				assert.Check(t, cmp.DeepEqual(server.GetMetrics(), tt.expectedMetricsData))
			})
		})
	}
}
