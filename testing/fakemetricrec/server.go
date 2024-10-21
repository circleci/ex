package fakemetricrec

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/httpserver/ginrouter"
)

type StubServer struct {
	mu      sync.RWMutex
	metrics []metricData

	errOnMetrics bool

	URL   string
	Close func()
}

func New(ctx context.Context) *StubServer {
	s := &StubServer{}
	s.start(ctx)
	return s
}

func (s *StubServer) start(ctx context.Context) {
	r := ginrouter.Default(ctx, "http-metric-stub")

	r.PUT("/metric", s.putMetric)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req)
	}))

	s.URL = server.URL
	s.Close = server.Close
}

type metrics struct {
	Data       []metricData `json:"metrics"`
	GlobalTags []string     `json:"tags"`
}

type metricData struct {
	Type  string   `json:"type"`
	Name  string   `json:"name"`
	Value float64  `json:"value"`
	Tags  []string `json:"tags"`
}

func (s *StubServer) putMetric(c *gin.Context) {
	if s.errOnMetrics {
		c.Status(http.StatusInternalServerError)
		return
	}

	var mp metrics
	err := c.BindJSON(&mp)
	if err != nil {
		c.JSON(http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = append(s.metrics, mp.Data...)
}

func (s *StubServer) SetErrOnMetrics(errOnMetrics bool) {
	s.errOnMetrics = errOnMetrics
}

func (s *StubServer) GetRawMetrics() []metricData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

func (s *StubServer) GetMetrics() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metrics := map[string]float64{}
	for _, metric := range s.metrics {
		if val, ok := metrics[metric.Name]; ok {
			metrics[metric.Name] = val + metric.Value
		} else {
			metrics[metric.Name] = metric.Value
		}
	}

	return metrics
}
