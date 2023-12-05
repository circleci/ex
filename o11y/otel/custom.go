package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var globalFields = Annotator{}

// Annotator is a SpanProcessor that adds attributes to all started spans.
type Annotator struct {
	attrs []attribute.KeyValue
}

func (a *Annotator) addField(key string, value interface{}) {
	a.attrs = append(a.attrs, attr(key, value))
}

func (a Annotator) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	s.SetAttributes(a.attrs...)
}
func (a Annotator) Shutdown(context.Context) error   { return nil }
func (a Annotator) ForceFlush(context.Context) error { return nil }
func (a Annotator) OnEnd(s sdktrace.ReadOnlySpan)    {}
