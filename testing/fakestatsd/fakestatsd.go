package fakestatsd

import (
	"bytes"
	"net"
	"strings"
	"sync"
	"testing"

	"gotest.tools/v3/assert"
)

type FakeStatsd struct {
	connection *net.UDPConn

	// mutable state
	mu      sync.RWMutex
	metrics []Metric
}

func New(t testing.TB) *FakeStatsd {
	t.Helper()

	addr, err := net.ResolveUDPAddr("udp", "localhost:0")
	assert.Assert(t, err)

	conn, err := net.ListenUDP("udp", addr)
	assert.Assert(t, err)

	s := &FakeStatsd{
		connection: conn,
	}
	go s.listen()
	t.Cleanup(s.close)

	return s
}

func (s *FakeStatsd) Addr() string {
	return s.connection.LocalAddr().String()
}

type Metric struct {
	Name  string
	Value string
	Tags  []string
}

func (s *FakeStatsd) Metrics() []Metric {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := make([]Metric, len(s.metrics))
	copy(metrics, s.metrics)
	return metrics
}

func (s *FakeStatsd) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metrics = nil
}

func (s *FakeStatsd) recordMetric(m Metric) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metrics = append(s.metrics, m)
}

func (s *FakeStatsd) listen() {
	buffer := make([]byte, 10000)

	for {
		numBytes, err := s.connection.Read(buffer)
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			break
		}

		rawMetrics := buffer[0:numBytes]
		splitMetrics := bytes.Split(rawMetrics, []byte("\n"))

		for _, rawMetric := range splitMetrics {
			rawMetric = bytes.TrimSpace(rawMetric)
			if len(rawMetric) == 0 {
				continue
			}
			metric := parse(string(rawMetric))
			s.recordMetric(metric)
		}
	}
}

func (s *FakeStatsd) close() {
	_ = s.connection.Close()
}

func parse(raw string) Metric {
	metricNameAndRest := strings.SplitN(raw, ":", 2)
	name := metricNameAndRest[0]
	valueAndTags := strings.SplitN(metricNameAndRest[1], "#", 2)
	value := valueAndTags[0]
	var tags []string

	if len(valueAndTags) > 1 {
		tags = strings.Split(valueAndTags[1], ",")
	}

	return Metric{Name: name, Value: value, Tags: tags}
}
