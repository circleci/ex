package httpmetrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/httpclient"
)

type Config struct {
	// URL configures a host for exporting metrics to http[s]://host[:port][/path]
	URL string
	// AuthToken is included as a bearer token on requests
	AuthToken secret.String
	// GlobalTags are added to each metric. Be aware of high cardinality issues
	GlobalTags Tags
	// ClientName provides a name & user agent to the http client for observability. Defaults to "o11y-metrics-client"
	ClientName string
	// PublishInterval how often to publish metrics, defaults to 1 minute
	PublishInterval time.Duration
	// Namespace gets prepended to all stat names
	Namespace string
}

// Provider is a struct that implements the CloseableMetricsProvider interface
type Provider struct {
	client           *httpclient.Client
	namespace        string
	globalMetricTags []string
	publishInterval  time.Duration
	mu               sync.RWMutex
	data             []metricData
	stop             chan bool
	stopMu           sync.Mutex
}

type metricData struct {
	Type  string   `json:"type"`
	Name  string   `json:"name"`
	Value float64  `json:"value"`
	Tags  []string `json:"tags"`
}

type Tags map[string]string

// New creates a Provider that implements the ClosableMetricsProvider interface
func New(cfg Config) *Provider {
	p := createProvider(cfg)
	p.startPublishLoop()
	return p
}

func createProvider(cfg Config) *Provider {
	tags := make([]string, 0, len(cfg.GlobalTags))
	for k, v := range cfg.GlobalTags {
		tags = append(tags, fmt.Sprintf("%s:%s", k, v))
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "http-metrics-client"
	}
	if cfg.PublishInterval == 0 {
		cfg.PublishInterval = time.Minute
	}
	return &Provider{
		data:             []metricData{},
		globalMetricTags: tags,
		publishInterval:  cfg.PublishInterval,
		namespace:        cfg.Namespace,
		client: httpclient.New(
			httpclient.Config{
				Name:       cfg.ClientName,
				BaseURL:    cfg.URL,
				UserAgent:  fmt.Sprintf("%s, ex", cfg.ClientName),
				AcceptType: httpclient.JSON,
				Timeout:    time.Millisecond * 500,
				AuthToken:  cfg.AuthToken.Raw(),
			}),
	}
}

// Gauge in an agent can be used for values that don't change,
func (m *Provider) Gauge(n string, v float64, t []string, rate float64) error {
	m.record("gauge", n, v, t)
	return nil
}

// Histogram can be used for any value that changes.
func (m *Provider) Histogram(n string, v float64, t []string, rate float64) error {
	m.record("histogram", n, v, t)
	return nil
}

// TimeInMilliseconds can be used for any timing data (recording how long something took)
func (m *Provider) TimeInMilliseconds(n string, v float64, t []string, rate float64) error {
	m.record("timeInMilliseconds", n, v, t)
	return nil
}

func (m *Provider) Count(n string, v int64, t []string, rate float64) error {
	m.record("count", n, float64(v), t)
	return nil
}

func (m *Provider) Close() error {
	m.stopMu.Lock()
	defer m.stopMu.Unlock()

	if m.stop != nil {
		close(m.stop)
		m.stop = nil

		ctx, done := context.WithTimeout(context.Background(), sendTimeout)
		defer done()
		m.Publish(ctx)
	}

	return nil
}

func (m *Provider) record(metricType, metricName string, metricValue float64, metricTags []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := metricName
	if m.namespace != "" {
		name = fmt.Sprintf("%s.%s", m.namespace, name)
	}
	m.data = append(m.data, metricData{
		Type:  metricType,
		Name:  name,
		Value: metricValue,
		Tags:  metricTags,
	})
}

const sendTimeout = 10 * time.Second

// startPublishLoop starts a loop which will publish metrics on an interval. It will attempt to flush data
// on close.
func (m *Provider) startPublishLoop() {
	m.stop = make(chan bool)

	go func() {
		ctx := context.Background()
		ticker := time.NewTicker(m.publishInterval)

		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
			}

			m.Publish(ctx)
		}
	}()
}

// Publish sends the stored metrics to receiver
func (m *Provider) Publish(ctx context.Context) {
	m.mu.Lock()
	metricsBackup := m.data
	sendingData := m.data
	m.data = []metricData{}
	m.mu.Unlock()

	if len(sendingData) == 0 {
		return
	}

	err := m.client.Call(ctx, httpclient.NewRequest("PUT", "",
		httpclient.Timeout(sendTimeout),
		httpclient.Body(
			struct {
				Data []metricData `json:"metrics"`
				Tags []string     `json:"tags"`
			}{
				Data: sendingData,
				Tags: m.globalMetricTags,
			},
		),
	))

	if err != nil {
		// reset metrics and replay anything written since backup was taken.
		m.mu.Lock()
		m.data = append(metricsBackup, m.data...)
		m.mu.Unlock()
	}
}
