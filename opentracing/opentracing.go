package opentracing

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/opentracing/opentracing-go"
	cfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"

	"github.com/circleci/distributor/o11y"
)

type client struct {
	tracer opentracing.Tracer
	closer io.Closer
}

func New(host string) *client {
	if host == "" {
		host = "http://localhost:14268/api/traces"
	}
	c := &cfg.Configuration{
		ServiceName: "distributor",
		Sampler: &cfg.SamplerConfig{
			Type:  "const",
			Param: 1,
		},
		Reporter: &cfg.ReporterConfig{
			LogSpans:          true,
			CollectorEndpoint: host,
		},
	}

	t, closer, err := c.NewTracer(cfg.Logger(jaegerlog.StdLogger))
	if err != nil {
		log.Fatal("cant create new tracer", err)
	}

	return &client{
		tracer: t,
		closer: closer,
	}
}

type span struct {
	s opentracing.Span
}

func (s *span) AddField(key string, val interface{}) {
	s.s.SetTag(key, val)
}

func (s *span) Send() {
	s.s.Finish()
}

func (c *client) GetSpanFromContext(ctx context.Context) o11y.Span {
	return &span{s: opentracing.SpanFromContext(ctx)}
}

func (c *client) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	s, newCtx := opentracing.StartSpanFromContextWithTracer(ctx, c.tracer, name)
	return newCtx, &span{s: s}
}

// AddFieldToTrace adds the val to the key on the top level tract that will be used in all child
// spans.
func (c *client) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	opentracing.SpanFromContext(ctx).SetBaggageItem(key, fmt.Sprintf("%v", val))
}

func (c *client) Flush(ctx context.Context) {
	// TODO - double check - that it is only the closer that will flush on close
}

func (c *client) Close(_ context.Context) {
	c.closer.Close()
}
