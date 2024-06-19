package jaeger

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	hc "github.com/circleci/ex/httpclient"
)

func New(base, service string) Client {
	return Client{
		cl: hc.New(hc.Config{
			Name:    "jaeger-api",
			BaseURL: base + "/api",
		}),
		service: service,
	}
}

type Client struct {
	cl      *hc.Client
	service string
}

type Tag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type Span struct {
	TraceID       string `json:"traceID"`
	SpanID        string `json:"spanID"`
	OperationName string `json:"operationName"`
	References    []struct {
		RefType string `json:"refType"`
		TraceID string `json:"traceID"`
		SpanID  string `json:"spanID"`
	} `json:"references"`
	StartTime int64  `json:"startTime"`
	Duration  int    `json:"duration"`
	Tags      []Tag  `json:"tags"`
	Logs      []any  `json:"logs"`
	ProcessID string `json:"processID"`
	Warnings  any    `json:"warnings"`
}

type Process struct {
	ServiceName string `json:"service_name"`
	Tags        []Tag  `json:"tags"`
}

type Trace struct {
	ID        string             `json:"id"`
	Spans     []Span             `json:"spans"`
	Processes map[string]Process `json:"processes"`
}

func (j *Client) Traces(ctx context.Context, since time.Time) ([]Trace, error) {
	resp := struct {
		Data []Trace `json:"data"`
	}{}
	err := j.cl.Call(ctx, hc.NewRequest("GET", "/traces",
		hc.QueryParam("service", j.service),
		hc.QueryParam("start", strconv.FormatInt(since.UnixMicro(), 10)),
		hc.JSONDecoder(&resp),
	))
	return resp.Data, err
}

func AssertTag(t *testing.T, tags []Tag, k, v string) {
	t.Helper()
	for _, tag := range tags {
		if tag.Key == k && fmt.Sprintf("%v", tag.Value) == v {
			return
		}
	}
	t.Errorf("key:%q with value %q was not found", k, v)
}
